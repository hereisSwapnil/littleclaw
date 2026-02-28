package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AppConfig holds the user's permanent API keys and model preferences.
type AppConfig struct {
	TelegramToken       string `json:"telegram_token"`
	TelegramAllowedUser string `json:"telegram_allowed_user"`
	ProviderType        string `json:"provider_type"`   // e.g. "openrouter", "ollama", "openai"
	ProviderModel       string `json:"provider_model"`  // e.g. "gpt-4o-mini", "llama3.2"
	ProviderAPIKey      string `json:"provider_apikey"` // (Empty for local Ollama)
}

// getConfigPath returns the absolute path to ~/.littleclaw/config.json
func getConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not find home directory: %w", err)
	}
	// Ensure the base directory exists
	dir := filepath.Join(home, ".littleclaw")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("could not create littleclaw directory: %w", err)
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads the config from disk.
func Load() (*AppConfig, error) {
	path, err := getConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config not found. Please run 'littleclaw configure' First")
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config JSON: %w", err)
	}

	return &cfg, nil
}

// Save writes the config back to disk securely.
func (cfg *AppConfig) Save() error {
	path, err := getConfigPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}

	// Save with strict permissions since it contains API keys (rw-------)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config to disk: %w", err)
	}
	
	return nil
}
