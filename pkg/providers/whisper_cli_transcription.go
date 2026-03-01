package providers

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// WhisperCLITranscriptionProvider implements TranscriptionProvider using the local whisper CLI.
type WhisperCLITranscriptionProvider struct {
	Model string
}

// NewWhisperCLITranscriptionProvider creates a new Whisper CLI transcription provider.
func NewWhisperCLITranscriptionProvider(model string) *WhisperCLITranscriptionProvider {
	if model == "" {
		model = "small"
	}
	return &WhisperCLITranscriptionProvider{
		Model: model,
	}
}

func (p *WhisperCLITranscriptionProvider) Transcribe(ctx context.Context, audioPath string) (string, error) {
	// Create a temporary directory for whisper output
	tmpDir, err := os.MkdirTemp("", "whisper_out_*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir for whisper: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Command: whisper <audioPath> --model <model> --output_dir <tmpDir> --output_format txt
	args := []string{
		audioPath,
		"--model", p.Model,
		"--output_dir", tmpDir,
		"--output_format", "txt",
	}

	log.Printf("🎙️ Running Whisper CLI: whisper %s", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "whisper", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("whisper CLI failed: %w\nOutput: %s", err, string(output))
	}
	log.Printf("✅ Whisper CLI finished successfully")

	// Read the output text file
	// Whisper creates <audio_filename>.txt
	base := filepath.Base(audioPath)
	ext := filepath.Ext(base)
	txtFile := filepath.Join(tmpDir, strings.TrimSuffix(base, ext)+".txt")

	content, err := os.ReadFile(txtFile)
	if err != nil {
		return "", fmt.Errorf("failed to read whisper output file: %w", err)
	}

	return strings.TrimSpace(string(content)), nil
}
