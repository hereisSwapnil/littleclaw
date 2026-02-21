package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// OpenAIProvider is a generic provider for OpenAI-compatible APIs.
// This supports OpenAI, OpenRouter, Ollama (with v1/chat/completions endpoint), and Codex.
type OpenAIProvider struct {
	NameStr    string
	BaseURL    string // e.g., "https://api.openai.com/v1" or "http://localhost:11434/v1"
	APIKey     string
	HTTPClient *http.Client
}

// NewOpenAIProvider creates a new provider compatible with OpenAI's API format.
func NewOpenAIProvider(name, baseURL, apiKey string) *OpenAIProvider {
	return &OpenAIProvider{
		NameStr:    name,
		BaseURL:    baseURL,
		APIKey:     apiKey,
		HTTPClient: &http.Client{},
	}
}

func (p *OpenAIProvider) Name() string {
	return p.NameStr
}

type openAIRequest struct {
	Model       string           `json:"model"`
	Messages    []openAIMessage  `json:"messages"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	Temperature float64          `json:"temperature,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
}

type openAIMessage struct {
	Role       string                   `json:"role"`
	Content    string                   `json:"content"`
	ToolCalls  []map[string]interface{} `json:"tool_calls,omitempty"`
	ToolCallID string                   `json:"tool_call_id,omitempty"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Role      string                   `json:"role"`
			Content   string                   `json:"content"`
			ToolCalls []map[string]interface{} `json:"tool_calls,omitempty"`
		} `json:"message"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
}

func (p *OpenAIProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	apiMessages := make([]openAIMessage, len(req.Messages))
	for i, msg := range req.Messages {
		apiMessages[i] = openAIMessage{
			Role:       msg.Role,
			Content:    msg.Content,
			ToolCalls:  msg.ToolCalls,
			ToolCallID: msg.ToolCallID,
		}
	}

	apiReq := openAIRequest{
		Model:       req.Model,
		Messages:    apiMessages,
		Tools:       req.Tools,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}

	bodyBytes, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	// For OpenRouter specific headers (not strictly necessary but good practice)
	if p.NameStr == "openrouter" {
		httpReq.Header.Set("HTTP-Referer", "https://littleclaw.local")
		httpReq.Header.Set("X-Title", "Littleclaw Agent")
	}

	resp, err := p.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices returned from API")
	}

	msg := apiResp.Choices[0].Message
	return &ChatResponse{
		Content:   msg.Content,
		ToolCalls: msg.ToolCalls,
		Usage:     apiResp.Usage,
	}, nil
}
