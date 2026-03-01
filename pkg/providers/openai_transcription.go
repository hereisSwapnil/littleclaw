package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// OpenAITranscriptionProvider implements TranscriptionProvider for OpenAI-compatible APIs (including local ones).
type OpenAITranscriptionProvider struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

// NewOpenAITranscriptionProvider creates a new OpenAI transcription provider.
func NewOpenAITranscriptionProvider(baseURL, apiKey, model string) *OpenAITranscriptionProvider {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "whisper-1"
	}
	return &OpenAITranscriptionProvider{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		Model:      model,
		HTTPClient: &http.Client{},
	}
}

type openAITranscriptionResponse struct {
	Text string `json:"text"`
}

func (p *OpenAITranscriptionProvider) Transcribe(ctx context.Context, audioPath string) (string, error) {
	file, err := os.Open(audioPath)
	if err != nil {
		return "", fmt.Errorf("failed to open audio file: %w", err)
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	
	part, err := writer.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", fmt.Errorf("failed to copy file to form: %w", err)
	}

	_ = writer.WriteField("model", p.Model)
	_ = writer.WriteField("response_format", "json")
	
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	url := strings.TrimSuffix(p.BaseURL, "/")
	
	// If the user provided the base URL only, we append the standard endpoint.
	// We handle both /v1-style and direct-style URLs broadly.
	var endpoint string
	if strings.HasSuffix(url, "/audio/transcriptions") {
		endpoint = url
	} else {
		endpoint = url + "/audio/transcriptions"
	}

	log.Printf("🎙️ Transcribing via: %s", endpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, body)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("OpenAI-compatible API error %d: %s", resp.StatusCode, string(respBody))
	}

	var oaResp openAITranscriptionResponse
	if err := json.NewDecoder(resp.Body).Decode(&oaResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return oaResp.Text, nil
}
