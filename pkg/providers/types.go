package providers

import (
	"context"
)

// Message represents a single message in a chat conversation.
type Message struct {
	Role       string                   `json:"role"`
	Content    string                   `json:"content"`
	ToolCalls  []map[string]interface{} `json:"tool_calls,omitempty"`
	ToolCallID string                   `json:"tool_call_id,omitempty"`
	Media      []string                 `json:"media,omitempty"` // Image URLs or local paths
}

// ToolDefinition represents a function the LLM can call.
type ToolDefinition struct {
	Type     string `json:"type"`
	Function struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		Parameters  map[string]interface{} `json:"parameters"`
	} `json:"function"`
}

// ChatRequest holds the necessary parameters for an LLM provider request.
type ChatRequest struct {
	Model       string
	Messages    []Message
	Tools       []ToolDefinition
	Temperature float64
	MaxTokens   int
}

// Usage holds token usage metrics if returned by the provider.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatResponse holds the parsed LLM response.
type ChatResponse struct {
	Content   string                   `json:"content"`
	ToolCalls []map[string]interface{} `json:"tool_calls,omitempty"`
	Usage     Usage                    `json:"usage"`
}

// Provider represents a generic LLM provider backend (OpenAI, Claude, OpenRouter, etc.)
type Provider interface {
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	Name() string
}
