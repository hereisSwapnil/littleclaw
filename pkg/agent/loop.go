package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"littleclaw/pkg/bus"
	"littleclaw/pkg/memory"
	"littleclaw/pkg/providers"
	"littleclaw/pkg/tools"
)

// NanoCore represents the central Agent ReAct Loop.
type NanoCore struct {
	provider     providers.Provider
	memoryStore  *memory.Store
	toolRegistry *tools.Registry
	msgBus       *bus.MessageBus
	workspace    string
	providerType string
	modelName    string
}

// NewNanoCore initializes the main agent brain.
func NewNanoCore(provider providers.Provider, providerType, modelName, workspace string, msgBus *bus.MessageBus) (*NanoCore, error) {
	memStore, err := memory.NewStore(workspace)
	if err != nil {
		return nil, fmt.Errorf("memory init failed: %w", err)
	}

	nc := &NanoCore{
		provider:     provider,
		memoryStore:  memStore,
		toolRegistry: tools.NewRegistry(workspace),
		msgBus:       msgBus,
		workspace:    workspace,
		providerType: providerType,
		modelName:    modelName,
	}

	nc.registerMemoryTools()

	return nc, nil
}

// RunAgentLoop processes an incoming user message through a multi-step reasoning loop.
func (c *NanoCore) RunAgentLoop(ctx context.Context, msg bus.InboundMessage) {
	// 1. Build initial context (System Prompt + Memory)
	sysPrompt := c.buildSystemPrompt()

	// 2. Initialize conversation history
	userPrompt := msg.Content
	if msg.ReplyTo != "" {
		userPrompt = fmt.Sprintf("Context (User is replying to this previous message):\n\"%s\"\n\nUser's message: %s", msg.ReplyTo, msg.Content)
	}

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
			c.sendResponse(msg.ChatID, msg.Channel, fmt.Sprintf("⚠ API Error: %v", err), nil)
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

				// Execute securely
				result := c.toolRegistry.Execute(ctx, toolName, args)

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
					c.sendResponse(msg.ChatID, msg.Channel, outMsg, result.Files)

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
			c.sendResponse(msg.ChatID, msg.Channel, resp.Content, nil)
			if msg.Channel == "internal" {
				c.memoryStore.AppendInternal("ASSISTANT", resp.Content)
			} else {
				c.memoryStore.AppendHistory("ASSISTANT", resp.Content)
			}
		}
		break
	}
	
	if iteration >= maxIterations {
		c.sendResponse(msg.ChatID, msg.Channel, "⚠ Reached maximum inference iterations.", nil)
	}
}

func (c *NanoCore) buildSystemPrompt() string {
	var builder strings.Builder
	builder.WriteString("You are Littleclaw, an ultra-fast, deeply personalized AI agent.\n")
	builder.WriteString("You have access to local file execution and scripts. Be concise, direct, and brilliant.\n\n")

	// Inject Hyper-Personalized Memory
	builder.WriteString(c.memoryStore.BuildContext())

	// Inject Short-Term Conversation Context
	recentHistory := c.memoryStore.ReadRecentHistory(4000) // ~1000 tokens of history
	if recentHistory != "" {
		builder.WriteString("\n\n## Recent Conversational History\n\n")
		builder.WriteString(recentHistory)
		builder.WriteString("\n\n(Note: The above is the recent conversation log. Use it to understand references like 'that file' or 'send it again'.)\n")
	}

	return builder.String()
}

func (c *NanoCore) sendResponse(chatID, channel, content string, files []string) {
	c.msgBus.SendOutbound(bus.OutboundMessage{
		Channel: channel,
		ChatID:  chatID,
		Content: content,
		Files:   files,
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
