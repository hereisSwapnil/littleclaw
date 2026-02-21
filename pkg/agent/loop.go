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
}

// NewNanoCore initializes the main agent brain.
func NewNanoCore(provider providers.Provider, workspace string, msgBus *bus.MessageBus) (*NanoCore, error) {
	memStore, err := memory.NewStore(workspace)
	if err != nil {
		return nil, fmt.Errorf("memory init failed: %w", err)
	}

	return &NanoCore{
		provider:     provider,
		memoryStore:  memStore,
		toolRegistry: tools.NewRegistry(workspace),
		msgBus:       msgBus,
		workspace:    workspace,
	}, nil
}

// RunAgentLoop processes an incoming user message through a multi-step reasoning loop.
func (c *NanoCore) RunAgentLoop(ctx context.Context, msg bus.InboundMessage) {
	// 1. Build initial context (System Prompt + Memory)
	sysPrompt := c.buildSystemPrompt()

	// 2. Initialize conversation history (in a real system, you'd load the recent session history here)
	messages := []providers.Message{
		{Role: "system", Content: sysPrompt},
		{Role: "user", Content: msg.Content}, // Omit media for brevity in this foundational version
	}

	// 3. Log user message to history
	c.memoryStore.AppendHistory("USER", msg.Content)

	maxIterations := 10
	iteration := 0

	for iteration < maxIterations {
		iteration++

		req := providers.ChatRequest{
			Model:       "gpt-4o-mini", // Fallback/Default test model, usually overridden by config
			Messages:    messages,
			Tools:       c.toolRegistry.GetDefinitions(),
			Temperature: 0.7,
		}

		resp, err := c.provider.Chat(ctx, req)
		if err != nil {
			c.sendResponse(msg.ChatID, msg.Channel, fmt.Sprintf("⚠ API Error: %v", err))
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

				// If the tool has direct user output (e.g., shell command execution logs)
				if result.ForUser != "" {
					c.sendResponse(msg.ChatID, msg.Channel, fmt.Sprintf("🛠 Tool `%s`: %s", toolName, result.ForUser))
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
			c.sendResponse(msg.ChatID, msg.Channel, resp.Content)
			c.memoryStore.AppendHistory("ASSISTANT", resp.Content)
		}
		break
	}
	
	if iteration >= maxIterations {
		c.sendResponse(msg.ChatID, msg.Channel, "⚠ Reached maximum inference iterations.")
	}
}

func (c *NanoCore) buildSystemPrompt() string {
	var builder strings.Builder
	builder.WriteString("You are Littleclaw, an ultra-fast, deeply personalized AI agent.\n")
	builder.WriteString("You have access to local file execution and scripts. Be concise, direct, and brilliant.\n\n")

	// Inject Hyper-Personalized Memory
	builder.WriteString(c.memoryStore.BuildContext())

	return builder.String()
}

func (c *NanoCore) sendResponse(chatID, channel, content string) {
	c.msgBus.SendOutbound(bus.OutboundMessage{
		Channel: channel,
		ChatID:  chatID,
		Content: content,
	})
}
