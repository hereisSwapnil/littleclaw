package providers

import (
	"context"
)

// TranscriptionProvider defines the interface for audio-to-text transcription.
type TranscriptionProvider interface {
	// Transcribe takes a local path to an audio file and returns its transcription.
	Transcribe(ctx context.Context, audioPath string) (string, error)
}
