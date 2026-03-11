package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"littleclaw/pkg/bus"
	"littleclaw/pkg/memory"
	"littleclaw/pkg/providers"
	"littleclaw/pkg/tools"
	"littleclaw/pkg/ui"
	"littleclaw/pkg/workspace"
)

type contextKey string

const (
	ctxChatID  contextKey = "chatID"
	ctxChannel contextKey = "channel"
)

// NanoCore represents the central Agent ReAct Loop.
type NanoCore struct {
	provider     providers.Provider
	memoryStore  *memory.Store
	toolRegistry *tools.Registry
	msgBus       *bus.MessageBus
	wsMgr        *workspace.Manager
	workspace    string
	providerType string
	modelName    string
	cronService  *CronService
	lastChatID   string
	lastChannel  string
	tavilyAPIKey string
	uiEvents     *ui.EventBus // Optional: emits events for the face UI
}

// SetUIEventBus attaches an event bus for the face UI.
func (c *NanoCore) SetUIEventBus(eb *ui.EventBus) {
	c.uiEvents = eb
	// Propagate to cron service
	if c.cronService != nil {
		c.cronService.SetUIEventBus(eb)
	}
}

// emitUI publishes a UI event if the event bus is attached.
func (c *NanoCore) emitUI(evtType ui.EventType, data interface{}) {
	if c.uiEvents == nil {
		return
	}
	c.uiEvents.Publish(ui.Event{
		Type: evtType,
		Data: data,
	})
}

// emitActivity logs an activity entry to the UI event bus.
func (c *NanoCore) emitActivity(kind, title, detail string) {
	if c.uiEvents == nil {
		return
	}
	c.uiEvents.AddActivity(ui.ActivityEntry{
		Kind:   kind,
		Title:  title,
		Detail: detail,
	})
}

// GetCronService returns the cron service for external access (e.g. UI stats).
func (c *NanoCore) GetCronService() *CronService {
	return c.cronService
}

// GetMemoryStore returns the memory store for external access (e.g. UI stats).
func (c *NanoCore) GetMemoryStore() *memory.Store {
	return c.memoryStore
}

// NewNanoCore initializes the main agent brain.
func NewNanoCore(provider providers.Provider, providerType, modelName, workspaceDir string, msgBus *bus.MessageBus, tavilyAPIKey string) (*NanoCore, error) {
	memStore, err := memory.NewStore(workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("memory init failed: %w", err)
	}

	wsMgr, err := workspace.NewManager(workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("workspace manager init failed: %w", err)
	}

	cronSvc := NewCronService(workspaceDir, msgBus, memStore)

	nc := &NanoCore{
		provider:     provider,
		memoryStore:  memStore,
		msgBus:       msgBus,
		wsMgr:        wsMgr,
		workspace:    workspaceDir,
		providerType: providerType,
		modelName:    modelName,
		cronService:  cronSvc,
		tavilyAPIKey: tavilyAPIKey,
	}

	// Initialize registry
	nc.toolRegistry = tools.NewRegistry(workspaceDir, memStore, wsMgr, tavilyAPIKey)

	nc.registerMemoryTools()
	nc.registerCronTools()
	nc.registerWorkspaceTools()

	return nc, nil
}

// RunAgentLoop processes an incoming user message through a multi-step reasoning loop.
func (c *NanoCore) RunAgentLoop(ctx context.Context, msg bus.InboundMessage) {
	// Update heartbeat so there's always a "last active" timestamp
	_ = c.memoryStore.UpdateHeartbeat()

	// If this is a real user message, track it for background task context
	if msg.ChatID != "internal_memory" && msg.ChatID != "" {
		c.lastChatID = msg.ChatID
		c.lastChannel = msg.Channel
	}

	// Inject ChatID and Channel into context for cron jobs/tools to use
	ctx = context.WithValue(ctx, ctxChatID, msg.ChatID)
	ctx = context.WithValue(ctx, ctxChannel, msg.Channel)

	// 1. Build initial context (System Prompt + Memory)
	sysPrompt := c.buildSystemPrompt()

	// 2. Initialize conversation history
	userPrompt := msg.Content
	if userPrompt == "" {
		// Log and avoid sending empty prompts to the model which can trigger native language hallucinations
		log.Printf("⚠️ Received empty message content from %s. Ignoring to prevent hallucination.", msg.SenderID)
		return
	}

	if msg.ReplyTo != "" {
		userPrompt = fmt.Sprintf("Context (User is replying to this previous message):\n\"%s\"\n\nUser's message: %s", msg.ReplyTo, msg.Content)
	}

	// Emit UI events for incoming message
	if msg.Channel != "internal" {
		c.emitUI(ui.EventMessageIn, map[string]interface{}{"message": userPrompt, "sender": msg.SenderID})
		c.emitActivity("message_in", "User Message", truncateStr(userPrompt, 100))
	}
	c.emitUI(ui.EventThinkingStart, map[string]interface{}{"message": userPrompt})

	messages := []providers.Message{
		{Role: "system", Content: sysPrompt},
		{Role: "user", Content: userPrompt}, // Omit media for brevity in this foundational version
	}

	// 3. Log user message to history
	if msg.Channel == "internal" {
		c.memoryStore.AppendInternal("SYSTEM", userPrompt)
	} else {
		c.memoryStore.AppendHistory("USER", userPrompt)
	}

	maxIterations := 10
	iteration := 0

	for iteration < maxIterations {
		iteration++

		req := providers.ChatRequest{
			Model:       c.modelName,
			Messages:    messages,
			Tools:       c.toolRegistry.GetDefinitions(),
			Temperature: 0.7,
		}

		resp, err := c.provider.Chat(ctx, req)
		if err != nil {
			c.sendResponse(msg.ChatID, msg.MessageID, msg.Channel, fmt.Sprintf("⚠ API Error: %v", err), nil)
			return
		}

		// Log Assistant Response internally (optional, for debug)
		// fmt.Printf("LLM Response: %+v\n", resp)

		if len(resp.ToolCalls) > 0 {
			// Add LLM's tool call intention to the message history
			messages = append(messages, providers.Message{
				Role:      "assistant",
				Content:   resp.Content,
				ToolCalls: resp.ToolCalls,
			})

			// Execute tools
			for _, tc := range resp.ToolCalls {
				toolName := tc["function"].(map[string]interface{})["name"].(string)
				argsStr := tc["function"].(map[string]interface{})["arguments"].(string)

				var args map[string]interface{}
				_ = json.Unmarshal([]byte(argsStr), &args)

				// Emit UI event for tool call
				c.emitUI(ui.EventToolCall, map[string]interface{}{"tool": toolName, "args": argsStr})
				c.emitActivity("tool_call", toolName, truncateStr(argsStr, 120))

				// Execute securely
				result := c.toolRegistry.Execute(ctx, toolName, args)

				// Emit UI event for tool result
				c.emitUI(ui.EventToolResult, map[string]interface{}{"tool": toolName, "result": truncateStr(result.ForLLM, 200)})

				// Append tool result to messages
				messages = append(messages, providers.Message{
					Role:       "tool",
					Content:    result.ForLLM,
					ToolCallID: tc["id"].(string),
				})

				// If the tool has direct user output (e.g., shell command execution logs) or files
				if result.ForUser != "" || len(result.Files) > 0 {
					outMsg := result.ForUser
					if toolName != "send_telegram_file" && result.ForUser != "" {
						outMsg = fmt.Sprintf("🛠 Tool `%s`: %s", toolName, result.ForUser)
					}
					c.sendResponse(msg.ChatID, msg.MessageID, msg.Channel, outMsg, result.Files)

					// Log tool outputs directly to memory history so the agent remembers
					historyMsg := outMsg
					if len(result.Files) > 0 {
						if historyMsg != "" {
							historyMsg += " "
						}
						historyMsg += fmt.Sprintf("[Attached files: %s]", strings.Join(result.Files, ", "))
					}

					if msg.Channel == "internal" {
						c.memoryStore.AppendInternal("ASSISTANT", historyMsg)
					} else {
						c.memoryStore.AppendHistory("ASSISTANT", historyMsg)
					}
				}
			}

			// Add a reflection prompt so the LLM decides what to do next
			messages = append(messages, providers.Message{
				Role:    "user",
				Content: "[System] Tool execution finished. Analyze the results and proceed or respond to the user.",
			})
			continue // Loop back and call LLM again
		}

		// If no tools, it's a final response
		if resp.Content != "" {
			c.emitUI(ui.EventResponseReady, map[string]interface{}{"message": resp.Content})
			c.emitActivity("message_out", "Bot Response", truncateStr(resp.Content, 120))
			c.sendResponse(msg.ChatID, msg.MessageID, msg.Channel, resp.Content, nil)
			if msg.Channel == "internal" {
				c.memoryStore.AppendInternal("ASSISTANT", resp.Content)
			} else {
				c.memoryStore.AppendHistory("ASSISTANT", resp.Content)
			}
		}
		break
	}

	// Emit thinking end
	c.emitUI(ui.EventThinkingEnd, nil)

	if iteration >= maxIterations {
		log.Printf("agent loop hit max iterations (%d) for chat %s", maxIterations, msg.ChatID)
	}
}

func (c *NanoCore) buildSystemPrompt() string {
	var builder strings.Builder
	// FORMATTING RULE must come first so the LLM sees it before anything else
	builder.WriteString("=== OUTPUT FORMAT RULE (MANDATORY) ===\n")
	builder.WriteString("You send messages over Telegram. Telegram does NOT render markdown syntax.\n")
	builder.WriteString("BANNED: ** (bold), __ (bold), # ## ### (headers), --- (dividers), _italic_.\n")
	builder.WriteString("BANNED: * bullet points. Use - instead.\n")
	builder.WriteString("BAD EXAMPLE: **Profile** | ## Skills | --- dividers | *emphasis*\n")
	builder.WriteString("GOOD EXAMPLE: Profile | Skills: | plain dashes for lists | CAPS for emphasis\n")
	builder.WriteString("ALWAYS write in plain text. Emoji are fine. Backticks for inline code are fine.\n")
	builder.WriteString("======================================\n\n")
	builder.WriteString("You are Littleclaw, an ultra-fast, deeply personalized AI agent.\n")
	builder.WriteString("You have access to local file execution and scripts. Be concise, direct, and brilliant.\n")
	builder.WriteString("MEMORY: Use `update_core_memory`, `list_entities`, `read_entity`, `write_entity` tools only — never write_file/append_file for memory.\n")
	builder.WriteString("WEB: Use `web_search` and `web_fetch` tools for real-time internet access.\n")

	// Workspace structure context
	builder.WriteString("\n=== WORKSPACE STRUCTURE ===\n")
	builder.WriteString("Your workspace is organized into structured folders. ALWAYS use the correct folder:\n")
	builder.WriteString("- scripts/   : shell and Python automation scripts (tracked in scripts/tracker.json)\n")
	builder.WriteString("- skills/    : executable scripts loaded as agent tools (tracked in skills/tracker.json)\n")
	builder.WriteString("- tools/     : utility programs and helpers (tracked in tools/tracker.json)\n")
	builder.WriteString("- memory/    : RESERVED — use memory tools only, never write_file here\n")
	builder.WriteString("Any other folders are custom and created on demand. Use `list_workspace` to see them.\n")
	builder.WriteString("Use `create_workspace_folder` to create a new folder for anything that needs its own space.\n")
	builder.WriteString("When writing a script, ALWAYS put it in scripts/ or skills/. NEVER dump files in the workspace root.\n")
	builder.WriteString("Use `track_item` to register scripts/tools with a description so you remember them later.\n")
	builder.WriteString("===========================\n")

	// Inject identity + personalized memory
	builder.WriteString(c.memoryStore.BuildContext())

	// Inject cron job run summaries so the agent knows what ran and when
	if summary := c.buildCronSummary(); summary != "" {
		builder.WriteString("\nScheduled Tasks - Recent Run Status:\n")
		builder.WriteString(summary)
	}

	// Inject Short-Term Conversation Context
	recentHistory := c.memoryStore.ReadRecentHistory(4000) // ~1000 tokens of history
	if recentHistory != "" {
		builder.WriteString("\nRecent Conversational History:\n")
		builder.WriteString(recentHistory)
		builder.WriteString("\n(Use this to understand references like 'that file' or 'send it again'.)\n")
	}

	return builder.String()
}

// buildCronSummary returns a compact text block describing all cron jobs and their last run.
func (c *NanoCore) buildCronSummary() string {
	jobs := c.cronService.ListJobs()
	if len(jobs) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, j := range jobs {
		statusEmoji := "⏳"
		if j.State.LastStatus == "ok" {
			statusEmoji = "✅"
		} else if j.State.LastStatus == "error" {
			statusEmoji = "❌"
		}

		lastRun := "never"
		if j.State.LastRunAtMs > 0 {
			lastRun = time.UnixMilli(j.State.LastRunAtMs).Format("2006-01-02 15:04 MST")
		}
		nextRun := "unknown"
		if j.State.NextRunAtMs > 0 {
			nextRun = time.UnixMilli(j.State.NextRunAtMs).Format("2006-01-02 15:04 MST")
		}

		sb.WriteString(fmt.Sprintf("- %s **%s** (ID: %s)\n", statusEmoji, j.Label, j.ID))
		sb.WriteString(fmt.Sprintf("  Schedule: %s | Last run: %s | Next run: %s", j.Schedule, lastRun, nextRun))
		if j.State.LastDurationMs > 0 {
			sb.WriteString(fmt.Sprintf(" | Duration: %dms", j.State.LastDurationMs))
		}
		if j.State.ConsecutiveErrors > 0 {
			sb.WriteString(fmt.Sprintf(" | ⚠️ %d consecutive error(s): %s", j.State.ConsecutiveErrors, j.State.LastError))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (c *NanoCore) sendResponse(chatID string, replyToMessageID int, channel, content string, files []string) {
	c.msgBus.SendOutbound(bus.OutboundMessage{
		Channel:          channel,
		ChatID:           chatID,
		ReplyToMessageID: replyToMessageID,
		Content:          content,
		Files:            files,
	})
}

// registerMemoryTools adds tools that interact directly with the memory store
func (c *NanoCore) registerMemoryTools() {
	// 1. update_core_memory
	c.toolRegistry.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "update_core_memory",
			Description: "Updates the long-term core memory profile (MEMORY.md). This permanently overrides the user's profile and preferences.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"content": map[string]interface{}{
						"type":        "string",
						"description": "The full, unstructured textual content representing the user's core memory facts.",
					},
				},
				"required": []string{"content"},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
		content, ok := args["content"].(string)
		if !ok {
			return &tools.ToolResult{ForLLM: "Error: content must be a string"}
		}

		if err := c.memoryStore.WriteLongTerm(content); err != nil {
			return &tools.ToolResult{ForLLM: fmt.Sprintf("Error updating core memory: %v", err)}
		}
		return &tools.ToolResult{ForLLM: "Successfully updated core memory (MEMORY.md)."}
	})

	// 2. read_entity
	c.toolRegistry.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "read_entity",
			Description: "Reads the deep contextual file for a specific entity (a person, place, project, or topic).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"entity_name": map[string]interface{}{
						"type":        "string",
						"description": "The name of the entity to look up (e.g., 'Alice_Smith', 'Project_Phoenix').",
					},
				},
				"required": []string{"entity_name"},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
		name, ok := args["entity_name"].(string)
		if !ok {
			return &tools.ToolResult{ForLLM: "Error: entity_name must be a string"}
		}

		data := c.memoryStore.ReadEntity(name)
		if data == "" {
			return &tools.ToolResult{ForLLM: fmt.Sprintf("No existing record found for entity: %s", name)}
		}
		return &tools.ToolResult{ForLLM: data}
	})

	// 3. write_entity
	c.toolRegistry.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "write_entity",
			Description: "Creates or updates a deeply-contextualized knowledge record for a specific entity.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"entity_name": map[string]interface{}{
						"type":        "string",
						"description": "The name of the entity.",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "The structured or unstructured information to save about the entity.",
					},
				},
				"required": []string{"entity_name", "content"},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
		name, okName := args["entity_name"].(string)
		content, okContent := args["content"].(string)
		if !okName || !okContent {
			return &tools.ToolResult{ForLLM: "Error: entity_name and content must be strings"}
		}

		if err := c.memoryStore.WriteEntity(name, content); err != nil {
			return &tools.ToolResult{ForLLM: fmt.Sprintf("Error writing entity: %v", err)}
		}
		return &tools.ToolResult{ForLLM: fmt.Sprintf("Successfully saved record for entity: %s", name)}
	})
}

// StartCronService starts the cron scheduler in the background.
func (c *NanoCore) StartCronService(ctx context.Context) {
	if err := c.cronService.Start(ctx); err != nil {
		fmt.Printf("⚠️ CronService failed to start: %v\n", err)
	}
}

// registerCronTools adds tools that allow the LLM to manage cron jobs.
func (c *NanoCore) registerCronTools() {
	// add_cron
	c.toolRegistry.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "add_cron",
			Description: "Schedule a recurring background task using a cron expression. The command is a shell command that runs inside the workspace on each tick. Its stdout will be sent directly to the user. Use '@every Xs' for intervals (e.g. '@every 10s', '@every 1h') or standard 5-field cron syntax.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"label": map[string]interface{}{
						"type":        "string",
						"description": "A short human-readable label for this task (e.g. 'joke_delivery').",
					},
					"schedule": map[string]interface{}{
						"type":        "string",
						"description": "The cron schedule. Examples: '@every 10s', '@every 1m', '@hourly', '*/5 * * * *'.",
					},
					"command": map[string]interface{}{
						"type":        "string",
						"description": "The shell command to run on each tick. Its stdout is sent to the user.",
					},
					"once": map[string]interface{}{
						"type":        "boolean",
						"description": "Set to true if this task should only run once and then be removed. Useful for one-time reminders.",
					},
				},
				"required": []string{"label", "schedule", "command"},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
		label, _ := args["label"].(string)
		schedule, _ := args["schedule"].(string)
		command, _ := args["command"].(string)
		once, _ := args["once"].(bool)

		if label == "" || schedule == "" || command == "" {
			return &tools.ToolResult{ForLLM: "Error: label, schedule, and command are all required."}
		}

		// Extract chatID and channel from context
		chatID, _ := ctx.Value(ctxChatID).(string)
		channel, _ := ctx.Value(ctxChannel).(string)

		// If we are in an internal loop (consolidation), use the last known user context
		if (chatID == "internal_memory" || chatID == "") && c.lastChatID != "" {
			chatID = c.lastChatID
			channel = c.lastChannel
		}

		if chatID == "internal_memory" || chatID == "" {
			return &tools.ToolResult{ForLLM: "Error: Cannot schedule cron job from internal context without a prior user interaction. Please wait for the user to message first."}
		}

		job := &CronJob{
			ID:       GenerateJobID(label),
			Label:    label,
			Schedule: schedule,
			Command:  command,
			ChatID:   chatID,
			Channel:  channel,
			Once:     once,
		}

		if err := c.cronService.AddJob(job); err != nil {
			return &tools.ToolResult{ForLLM: fmt.Sprintf("Failed to add cron job: %v", err)}
		}

		return &tools.ToolResult{
			ForLLM: fmt.Sprintf("Cron job '%s' scheduled successfully (ID: %s, schedule: %s).", label, job.ID, schedule),
		}
	})

	// remove_cron
	c.toolRegistry.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "remove_cron",
			Description: "Stop and remove a scheduled cron task by its ID or Label. Use list_cron to see active tasks.",
			Parameters: map[string]interface{}{
				"job_id": map[string]interface{}{
					"type":        "string",
					"description": "The ID or Label of the cron job to remove.",
				},
				"required": []string{"job_id"},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
		jobID, _ := args["job_id"].(string)
		if jobID == "" {
			return &tools.ToolResult{ForLLM: "Error: job_id is required."}
		}

		err := c.cronService.RemoveJob(jobID)
		if err != nil {
			// Try finding by label if direct ID removal fails
			jobs := c.cronService.ListJobs()
			found := false
			for _, j := range jobs {
				if j.Label == jobID || j.ID == jobID {
					if errRem := c.cronService.RemoveJob(j.ID); errRem == nil {
						found = true
						break
					}
				}
			}
			if !found {
				return &tools.ToolResult{ForLLM: fmt.Sprintf("Failed to remove cron job: %v", err)}
			}
		}

		return &tools.ToolResult{ForLLM: fmt.Sprintf("Cron job '%s' removed successfully.", jobID)}
	})

	// list_cron
	c.toolRegistry.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "list_cron",
			Description: "List all currently scheduled cron jobs, including their IDs, labels, schedules, last-run time, status, duration, and next scheduled run.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
		jobs := c.cronService.ListJobs()
		if len(jobs) == 0 {
			return &tools.ToolResult{ForLLM: "No cron jobs are currently scheduled."}
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("%d scheduled cron job(s):\n\n", len(jobs)))
		for _, j := range jobs {
			statusEmoji := "⏳ never run"
			if j.State.LastStatus == "ok" {
				statusEmoji = "✅ ok"
			} else if j.State.LastStatus == "error" {
				statusEmoji = "❌ error"
			}

			lastRun := "never"
			if j.State.LastRunAtMs > 0 {
				lastRun = time.UnixMilli(j.State.LastRunAtMs).Format("2006-01-02 15:04:05")
			}
			nextRun := "unknown"
			if j.State.NextRunAtMs > 0 {
				nextRun = time.UnixMilli(j.State.NextRunAtMs).Format("2006-01-02 15:04:05")
			}

			sb.WriteString(fmt.Sprintf("**%s** (ID: `%s`)\n", j.Label, j.ID))
			sb.WriteString(fmt.Sprintf("  Schedule:  %s\n", j.Schedule))
			sb.WriteString(fmt.Sprintf("  Command:   %s\n", j.Command))
			sb.WriteString(fmt.Sprintf("  Status:    %s\n", statusEmoji))
			sb.WriteString(fmt.Sprintf("  Last run:  %s", lastRun))
			if j.State.LastDurationMs > 0 {
				sb.WriteString(fmt.Sprintf(" (%dms)", j.State.LastDurationMs))
			}
			sb.WriteString("\n")
			sb.WriteString(fmt.Sprintf("  Next run:  %s\n", nextRun))
			if j.State.ConsecutiveErrors > 0 {
				sb.WriteString(fmt.Sprintf("  ⚠️  %d consecutive error(s) — last: %s\n", j.State.ConsecutiveErrors, j.State.LastError))
			}
			if j.Once {
				sb.WriteString("  One-time: yes (removed after first run)\n")
			}
			sb.WriteString("\n")
		}
		return &tools.ToolResult{ForLLM: sb.String()}
	})
}

// truncateStr shortens a string to maxLen, appending "..." if truncated.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
