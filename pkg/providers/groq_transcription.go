package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
)

// GroqTranscriptionProvider implements TranscriptionProvider for Groq's Whisper API.
type GroqTranscriptionProvider struct {
	APIKey     string
	HTTPClient *http.Client
}

// NewGroqTranscriptionProvider creates a new Groq transcription provider.
func NewGroqTranscriptionProvider(apiKey string) *GroqTranscriptionProvider {
	return &GroqTranscriptionProvider{
		APIKey:     apiKey,
		HTTPClient: &http.Client{},
	}
}

type groqTranscriptionResponse struct {
	Text string `json:"text"`
}

func (p *GroqTranscriptionProvider) Transcribe(ctx context.Context, audioPath string) (string, error) {
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

	_ = writer.WriteField("model", "whisper-large-v3")
	_ = writer.WriteField("response_format", "json")
	
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.groq.com/openai/v1/audio/transcriptions", body)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Groq API error %d: %s", resp.StatusCode, string(respBody))
	}

	var groqResp groqTranscriptionResponse
	if err := json.NewDecoder(resp.Body).Decode(&groqResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return groqResp.Text, nil
}
