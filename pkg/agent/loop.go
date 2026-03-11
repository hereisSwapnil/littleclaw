package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"littleclaw/pkg/bus"
	"littleclaw/pkg/memory"
	"littleclaw/pkg/providers"
	"littleclaw/pkg/tools"
	"littleclaw/pkg/workspace"
)

type contextKey string

const (
	ctxChatID  contextKey = "chatID"
	ctxChannel contextKey = "channel"

	// Context budget constants (in estimated tokens; 1 token ~= 4 chars)
	maxContextTokens     = 8000  // total token budget for the system prompt
	identityBudgetTokens = 800   // identity files (SOUL, IDENTITY, USER)
	coreBudgetTokens     = 2000  // MEMORY.md
	historyBudgetBytes   = 16000 // ~4000 tokens, expanded from 4000 bytes
	entityBudgetTokens   = 800   // auto-surfaced entities
	cronBudgetTokens     = 400   // cron summaries

	// charsPerToken is the default ratio for the simple truncation helper.
	// The more sophisticated memory.EstimateTokens() is used where precision matters.
	charsPerToken = 4

	// maxToolResultChars caps the length of a single tool result in the messages array.
	maxToolResultChars = 3000

	// preCompactionThreshold: when prompt tokens exceed this fraction of the model's
	// apparent context window, trigger an early memory consolidation.
	preCompactionThreshold = 0.80
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
	tavilyAPIKey string

	// Protected by chatMu for concurrent goroutine access
	chatMu      sync.Mutex
	lastChatID  string
	lastChannel string

	// Pre-compaction tracking
	lastPromptTokens int
	contextWindowEst int // estimated context window for the model (set on first API response)
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
		c.chatMu.Lock()
		c.lastChatID = msg.ChatID
		c.lastChannel = msg.Channel
		c.chatMu.Unlock()
	}

	// Inject ChatID and Channel into context for cron jobs/tools to use
	ctx = context.WithValue(ctx, ctxChatID, msg.ChatID)
	ctx = context.WithValue(ctx, ctxChannel, msg.Channel)

	// 1. Initialize user prompt first (needed for entity auto-surfacing)
	userPrompt := msg.Content
	if userPrompt == "" {
		// Log and avoid sending empty prompts to the model which can trigger native language hallucinations
		log.Printf("⚠️ Received empty message content from %s. Ignoring to prevent hallucination.", msg.SenderID)
		return
	}

	if msg.ReplyTo != "" {
		userPrompt = fmt.Sprintf("Context (User is replying to this previous message):\n\"%s\"\n\nUser's message: %s", msg.ReplyTo, msg.Content)
	}

	// 2. Build initial context (System Prompt + Memory), using the user message for entity surfacing
	sysPrompt := c.buildSystemPromptWithQuery(msg.Content)

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

		// Log token usage for observability and adaptive context sizing
		if resp.Usage.TotalTokens > 0 {
			log.Printf("📊 Token usage: prompt=%d completion=%d total=%d (iteration %d)",
				resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens, iteration)

			// Track for pre-compaction awareness
			c.lastPromptTokens = resp.Usage.PromptTokens
			if c.contextWindowEst == 0 && resp.Usage.PromptTokens > 0 {
				// Heuristic: estimate context window from first response.
				// Most models use 128k, but we use a conservative estimate.
				c.contextWindowEst = estimateContextWindow(c.modelName)
			}
		}

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

				// Execute securely
				result := c.toolRegistry.Execute(ctx, toolName, args)

				// Append tool result to messages (truncated to prevent context blowup)
				messages = append(messages, providers.Message{
					Role:       "tool",
					Content:    truncateToolResult(result.ForLLM),
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
			c.sendResponse(msg.ChatID, msg.MessageID, msg.Channel, resp.Content, nil)
			if msg.Channel == "internal" {
				c.memoryStore.AppendInternal("ASSISTANT", resp.Content)
			} else {
				c.memoryStore.AppendHistory("ASSISTANT", resp.Content)
			}
		}
		break
	}

	if iteration >= maxIterations {
		log.Printf("agent loop hit max iterations (%d) for chat %s", maxIterations, msg.ChatID)
	}
}

func (c *NanoCore) buildSystemPrompt() string {
	return c.buildSystemPromptWithQuery("")
}

// buildSystemPromptWithQuery assembles the full system prompt with token-budgeted sections.
// The optional query is used for lightweight entity auto-surfacing.
func (c *NanoCore) buildSystemPromptWithQuery(query string) string {
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
	builder.WriteString("MEMORY: Use `update_core_memory`, `append_core_memory`, `read_core_memory`, `search_history`, `list_entities`, `read_entity`, `write_entity`, `read_internal_log` tools only — never write_file/append_file for memory.\n")
	builder.WriteString("MEMORY BEST PRACTICES:\n")
	builder.WriteString("- Prefer `append_core_memory` for adding new facts. Only use `update_core_memory` when reorganizing/cleaning up.\n")
	builder.WriteString("- Always `read_core_memory` before `update_core_memory` to avoid losing existing information.\n")
	builder.WriteString("- Use `search_history` to recall past conversations before guessing.\n")
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

	// Inject identity + personalized memory (token-budgeted)
	identityCtx := c.memoryStore.ReadIdentityContext()
	identityCtx = truncateToTokenBudget(identityCtx, identityBudgetTokens)
	if identityCtx != "" {
		builder.WriteString(identityCtx)
		builder.WriteString("\n\n")
	}

	coreMemory := c.memoryStore.ReadLongTerm()
	coreMemory = truncateToTokenBudget(coreMemory, coreBudgetTokens)
	if coreMemory != "" {
		builder.WriteString("## Personal Context & Memory\n\n")
		builder.WriteString(coreMemory)
		builder.WriteString("\n\n")

		// Warn if core memory is approaching budget limits
		coreSize := c.memoryStore.CoreMemorySize()
		budgetBytes := int64(coreBudgetTokens * charsPerToken)
		if coreSize > budgetBytes {
			builder.WriteString(fmt.Sprintf("⚠️ MEMORY.md (%d bytes) exceeds budget (%d bytes). Consider using `update_core_memory` to reorganize and deduplicate.\n\n",
				coreSize, budgetBytes))
		}
	}

	// Inject cron job run summaries so the agent knows what ran and when
	if summary := c.buildCronSummary(); summary != "" {
		summary = truncateToTokenBudget(summary, cronBudgetTokens)
		builder.WriteString("\nScheduled Tasks - Recent Run Status:\n")
		builder.WriteString(summary)
	}

	// Auto-surface relevant entities based on user query (trigram + keyword similarity)
	if query != "" {
		entityCtx := c.memoryStore.FindRelevantEntities(query, entityBudgetTokens*charsPerToken)
		if entityCtx != "" {
			builder.WriteString("\n\n=== RELEVANT ENTITY CONTEXT ===\n")
			builder.WriteString(entityCtx)
			builder.WriteString("\n===============================\n")
		}
	}

	// Inject Short-Term Conversation Context from daily logs
	recentHistory := c.memoryStore.ReadRecentHistory(historyBudgetBytes)
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

// truncateToTokenBudget truncates a string to fit within the given token budget.
// Uses a rough estimate of charsPerToken characters per token.
func truncateToTokenBudget(s string, maxTokens int) string {
	maxChars := maxTokens * charsPerToken
	if len(s) <= maxChars {
		return s
	}
	// Truncate at a word boundary if possible
	truncated := s[:maxChars]
	lastSpace := strings.LastIndex(truncated, " ")
	if lastSpace > maxChars/2 {
		truncated = truncated[:lastSpace]
	}
	return truncated + "\n...(truncated to fit context budget)"
}

// truncateToolResult caps a tool result string to avoid blowing up the message array.
func truncateToolResult(s string) string {
	if len(s) <= maxToolResultChars {
		return s
	}
	return s[:maxToolResultChars] + "\n...(truncated)"
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

// IsApproachingContextLimit returns true if the last observed prompt token usage
// exceeds preCompactionThreshold of the estimated context window.
func (c *NanoCore) IsApproachingContextLimit() bool {
	if c.contextWindowEst == 0 || c.lastPromptTokens == 0 {
		return false
	}
	ratio := float64(c.lastPromptTokens) / float64(c.contextWindowEst)
	return ratio >= preCompactionThreshold
}

// estimateContextWindow returns a conservative context window estimate for common models.
func estimateContextWindow(modelName string) int {
	modelLower := strings.ToLower(modelName)
	switch {
	case strings.Contains(modelLower, "gpt-4o"),
		strings.Contains(modelLower, "gpt-4-turbo"):
		return 128000
	case strings.Contains(modelLower, "gpt-4"):
		return 8192
	case strings.Contains(modelLower, "gpt-3.5"):
		return 16385
	case strings.Contains(modelLower, "claude-3"),
		strings.Contains(modelLower, "claude-4"):
		return 200000
	case strings.Contains(modelLower, "gemini"):
		return 128000
	case strings.Contains(modelLower, "llama"),
		strings.Contains(modelLower, "mixtral"):
		return 32768
	default:
		return 32768 // safe conservative default
	}
}

// registerMemoryTools adds tools that interact directly with the memory store
func (c *NanoCore) registerMemoryTools() {
	// 1. update_core_memory -- full overwrite (with backup)
	c.toolRegistry.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "update_core_memory",
			Description: "Replaces the ENTIRE long-term core memory (MEMORY.md). Creates a backup first. IMPORTANT: Always read_core_memory first to avoid losing existing information. Only use this for reorganizing or deduplicating. For adding new facts, prefer append_core_memory.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"content": map[string]interface{}{
						"type":        "string",
						"description": "The full content to replace MEMORY.md with. Must include ALL facts you want to keep.",
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
		return &tools.ToolResult{ForLLM: "Successfully updated core memory (MEMORY.md). A backup of the previous version was created."}
	})

	// 1b. append_core_memory — incremental fact addition without full overwrite
	c.toolRegistry.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "append_core_memory",
			Description: "Appends new facts or preferences to the core memory (MEMORY.md) WITHOUT overwriting existing content. Use this for incremental updates. Use update_core_memory only when you need to reorganize or clean up the entire memory.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"content": map[string]interface{}{
						"type":        "string",
						"description": "The new facts or information to append to the existing core memory.",
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

		if err := c.memoryStore.AppendLongTerm(content); err != nil {
			return &tools.ToolResult{ForLLM: fmt.Sprintf("Error appending to core memory: %v", err)}
		}

		// Check if memory is getting large and warn
		size := c.memoryStore.CoreMemorySize()
		budgetBytes := int64(coreBudgetTokens * charsPerToken)
		warning := ""
		if size > budgetBytes {
			warning = fmt.Sprintf(" WARNING: MEMORY.md is now %d bytes (budget: %d bytes). Consider using update_core_memory to reorganize and deduplicate.", size, budgetBytes)
		}

		return &tools.ToolResult{ForLLM: "Successfully appended to core memory (MEMORY.md)." + warning}
	})

	// 1c. read_core_memory — read current core memory before updating
	c.toolRegistry.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "read_core_memory",
			Description: "Reads the current contents of core memory (MEMORY.md). Use this before update_core_memory to ensure you don't lose existing information.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
		content := c.memoryStore.ReadLongTerm()
		if content == "" {
			return &tools.ToolResult{ForLLM: "Core memory (MEMORY.md) is currently empty."}
		}
		size := c.memoryStore.CoreMemorySize()
		header := fmt.Sprintf("[MEMORY.md — %d bytes]\n\n", size)
		return &tools.ToolResult{ForLLM: header + content}
	})

	// 2. search_history — search across daily logs and archives
	c.toolRegistry.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "search_history",
			Description: "Searches conversation history across all daily logs and archives for a query string. Use this to recall past conversations, find what the user said about a topic, or recover context from previous sessions.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "The search term or phrase to find in conversation history.",
					},
					"from_date": map[string]interface{}{
						"type":        "string",
						"description": "Optional start date filter (YYYY-MM-DD format). Only search logs from this date onward.",
					},
					"to_date": map[string]interface{}{
						"type":        "string",
						"description": "Optional end date filter (YYYY-MM-DD format). Only search logs up to this date.",
					},
				},
				"required": []string{"query"},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
		query, ok := args["query"].(string)
		if !ok || query == "" {
			return &tools.ToolResult{ForLLM: "Error: query must be a non-empty string"}
		}

		fromDate, _ := args["from_date"].(string)
		toDate, _ := args["to_date"].(string)

		results := c.memoryStore.SearchHistory(query, fromDate, toDate)
		if len(results) == 0 {
			return &tools.ToolResult{ForLLM: fmt.Sprintf("No matches found for '%s' in conversation history.", query)}
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Found %d match(es) for '%s':\n\n", len(results), query))
		for i, r := range results {
			sb.WriteString(fmt.Sprintf("--- Match %d [%s] ---\n%s\n\n", i+1, r.Date, r.Content))
		}
		return &tools.ToolResult{ForLLM: sb.String()}
	})

	// 3. read_entity
	c.toolRegistry.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "read_entity",
			Description: "Reads the deep contextual file for a specific entity (a person, place, project, or topic). Entity names are normalized (case-insensitive, spaces become underscores).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"entity_name": map[string]interface{}{
						"type":        "string",
						"description": "The name of the entity to look up (e.g., 'Alice Smith', 'project_phoenix').",
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

	// 4. write_entity
	c.toolRegistry.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "write_entity",
			Description: "Creates or updates a deeply-contextualized knowledge record for a specific entity. Entity names are automatically normalized (lowercase, underscores).",
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

	// 5. write_summary -- save a summarized digest of a daily log
	c.toolRegistry.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "write_summary",
			Description: "Saves a summarized digest of a daily conversation log. Used during automatic summarization of large daily logs.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"date": map[string]interface{}{
						"type":        "string",
						"description": "The date of the log being summarized (YYYY-MM-DD format).",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "The summarized digest of that day's conversations.",
					},
				},
				"required": []string{"date", "content"},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
		date, okDate := args["date"].(string)
		content, okContent := args["content"].(string)
		if !okDate || !okContent {
			return &tools.ToolResult{ForLLM: "Error: date and content must be strings"}
		}

		if err := c.memoryStore.WriteSummary(date, content); err != nil {
			return &tools.ToolResult{ForLLM: fmt.Sprintf("Error writing summary: %v", err)}
		}
		return &tools.ToolResult{ForLLM: fmt.Sprintf("Successfully saved summary for %s.", date)}
	})

	// 6. read_internal_log -- review recent background reasoning and cron outputs
	c.toolRegistry.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "read_internal_log",
			Description: "Reads the most recent entries from the internal operations log (INTERNAL.md). Contains background reasoning, cron job outputs, and consolidation records. Useful for debugging or reviewing what happened in the background.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
		content := c.memoryStore.ReadRecentInternal()
		if content == "" {
			return &tools.ToolResult{ForLLM: "Internal log (INTERNAL.md) is empty or does not exist yet."}
		}
		return &tools.ToolResult{ForLLM: "[Recent Internal Log]\n\n" + content}
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
		if chatID == "internal_memory" || chatID == "" {
			c.chatMu.Lock()
			chatID = c.lastChatID
			channel = c.lastChannel
			c.chatMu.Unlock()
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
