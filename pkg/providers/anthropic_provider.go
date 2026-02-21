package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// AnthropicProvider integrates with Claude APIs via Anthropic's Messages API
type AnthropicProvider struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

func NewAnthropicProvider(apiKey string) *AnthropicProvider {
	return &AnthropicProvider{
		APIKey:     apiKey,
		BaseURL:    "https://api.anthropic.com/v1",
		HTTPClient: &http.Client{},
	}
}

func (p *AnthropicProvider) Name() string {
	return "anthropic"
}

type anthropicRequest struct {
	Model       string                 `json:"model"`
	Messages    []anthropicMessage     `json:"messages"`
	System      string                 `json:"system,omitempty"`
	MaxTokens   int                    `json:"max_tokens"`
	Temperature float64                `json:"temperature"`
	Tools       []anthropicTool        `json:"tools,omitempty"`
}

type anthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or array of parts
}

type anthropicTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

func (p *AnthropicProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	var messages []anthropicMessage
	var systemPrompt string

	// Anthropic treats "system" uniquely as a top-level field, not in messages array.
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			systemPrompt = msg.Content
		} else {
			messages = append(messages, anthropicMessage{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
	}

	var anthropicTools []anthropicTool
	for _, t := range req.Tools {
		anthropicTools = append(anthropicTools, anthropicTool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		})
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096 // Anthropic requires max_tokens
	}

	apiReq := anthropicRequest{
		Model:       req.Model,
		Messages:    messages,
		System:      systemPrompt,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		Tools:       anthropicTools,
	}

	bodyBytes, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/messages", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Anthropic API error %d: %s", resp.StatusCode, string(respBody))
	}

	// For simplicity, defining a basic response map instead of full struct to handle content extraction
	var apiResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
			Id   string `json:"id,omitempty"`
			Name string `json:"name,omitempty"`
			Input map[string]interface{} `json:"input,omitempty"`
		} `json:"content"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var textContent string
	var toolCalls []map[string]interface{}

	for _, block := range apiResp.Content {
		if block.Type == "text" {
			textContent += block.Text
		} else if block.Type == "tool_use" {
			argumentsBytes, _ := json.Marshal(block.Input)
			toolCalls = append(toolCalls, map[string]interface{}{
				"id": block.Id,
				"function": map[string]interface{}{
					"name": block.Name,
					"arguments": string(argumentsBytes),
				},
				"type": "function",
			})
		}
	}

	return &ChatResponse{
		Content:   textContent,
		ToolCalls: toolCalls,
	}, nil
}
