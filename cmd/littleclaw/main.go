package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"littleclaw/pkg/agent"
	"littleclaw/pkg/bus"
	"littleclaw/pkg/channels/telegram"
	"littleclaw/pkg/config"
	"littleclaw/pkg/providers"

	"github.com/joho/godotenv"
	"github.com/manifoldco/promptui"
)

func promptWithDefault(label string, defaultValue string) string {
	prompt := promptui.Prompt{
		Label:   label,
		Default: defaultValue,
	}

	result, err := prompt.Run()
	if err != nil {
		return defaultValue
	}
	return result
}

func selectOption(label string, options []string, defaultValue string) string {
	cursorPos := 0
	for i, opt := range options {
		if opt == defaultValue {
			cursorPos = i
			break
		}
	}

	prompt := promptui.Select{
		Label:     label,
		Items:     options,
		CursorPos: cursorPos,
	}

	_, result, err := prompt.Run()
	if err != nil {
		return defaultValue
	}
	return result
}

func runConfigure() {
	fmt.Println("🦐 Littleclaw Configuration Wizard")
	fmt.Println("---------------------------------")

	// Load existing if possible
	cfg, err := config.Load()
	if err != nil {
		cfg = &config.AppConfig{}
	}

	cfg.TelegramToken = promptWithDefault("Enter Telegram Bot Token", cfg.TelegramToken)
	cfg.TelegramAllowedUser = promptWithDefault("Enter Restricted Telegram User ID (Optional)", cfg.TelegramAllowedUser)

	providerOptions := []string{"openrouter", "ollama", "openai", "anthropic"}
	cfg.ProviderType = selectOption("Choose LLM Provider", providerOptions, cfg.ProviderType)

	if cfg.ProviderType == "ollama" {
		cfg.ProviderModel = promptWithDefault("Enter Ollama Model (e.g. llama3.2)", cfg.ProviderModel)
	} else {
		cfg.ProviderAPIKey = promptWithDefault(fmt.Sprintf("Enter %s API Key", cfg.ProviderType), cfg.ProviderAPIKey)
		cfg.ProviderModel = promptWithDefault("Enter Model Name (e.g. gpt-4o-mini)", cfg.ProviderModel)
	}

	transcriberOptions := []string{"groq", "openai", "whisper-cli", "none"}
	cfg.TranscriptionProvider = selectOption("Choose Transcription Provider", transcriberOptions, cfg.TranscriptionProvider)

	if cfg.TranscriptionProvider != "none" {
		if cfg.TranscriptionProvider == "openai" {
			cfg.TranscriptionBaseURL = promptWithDefault("Enter OpenAI/Local Whisper Base URL (e.g. http://localhost:8080/v1)", cfg.TranscriptionBaseURL)
			if cfg.TranscriptionBaseURL == "" {
				cfg.TranscriptionBaseURL = "http://localhost:8080/v1"
			}
		}

		if cfg.TranscriptionProvider == "openai" || cfg.TranscriptionProvider == "whisper-cli" {
			cfg.TranscriptionModel = promptWithDefault("Enter Whisper Model (e.g. whisper-1, base, small)", cfg.TranscriptionModel)
			if cfg.TranscriptionModel == "" {
				cfg.TranscriptionModel = "small"
			}
		}

		if cfg.TranscriptionProvider != "whisper-cli" {
			cfg.TranscriptionAPIKey = promptWithDefault(fmt.Sprintf("Enter %s API Key", cfg.TranscriptionProvider), cfg.TranscriptionAPIKey)
		}
	}

	fmt.Println("\n🔍 Testing Provider Connection...")
	
	// Create temporary provider to verify settings before saving
	var provider providers.Provider
	if cfg.ProviderType == "ollama" {
		provider = providers.NewOpenAIProvider("ollama", "http://localhost:11434/v1", "dummy")
	} else if cfg.ProviderType == "openrouter" {
		provider = providers.NewOpenAIProvider("openrouter", "https://openrouter.ai/api/v1", cfg.ProviderAPIKey)
	} else if cfg.ProviderType == "openai" {
		provider = providers.NewOpenAIProvider("openai", "https://api.openai.com/v1", cfg.ProviderAPIKey)
	}

	if provider != nil {
		req := providers.ChatRequest{
			Model: cfg.ProviderModel,
			Messages: []providers.Message{ {Role: "user", Content: "Say 'OK' if you can read this."} },
			MaxTokens: 10,
		}
		
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		
		_, err := provider.Chat(ctx, req)
		if err != nil {
			fmt.Printf("❌ Failed to verify provider: %v\n", err)
			fmt.Print("⚠️  Do you want to save the configuration anyway? (y/N): ")
			var confirmSave string
			fmt.Scanln(&confirmSave)
			if confirmSave != "y" && confirmSave != "Y" {
				fmt.Println("Configuration not saved.")
				return
			}
		} else {
			fmt.Println("✅ Connection successful!")
		}
	} else {
		fmt.Println("⚠️ Unknown provider type, saving config without verification.")
	}

	if err := cfg.Save(); err != nil {
		log.Fatalf("❌ Failed to save config: %v", err)
	}
	
	fmt.Println("✅ Configuration saved successfully to ~/.littleclaw/config.json!")
	fmt.Println("You can now run 'go run cmd/littleclaw/main.go' to start the agent.")
}

func runReset() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Cannot get home dir: %v", err)
	}
	workspaceDir := filepath.Join(home, ".littleclaw", "workspace")

	fmt.Printf("🗑️ Are you sure you want to reset Littleclaw's entire workspace? This will delete all memory, history, entities, and downloaded files in %s. (y/N): ", workspaceDir)
	var confirm string
	fmt.Scanln(&confirm)
	if confirm != "y" && confirm != "Y" {
		fmt.Println("Reset cancelled.")
		return
	}

	if err := os.RemoveAll(workspaceDir); err != nil {
		log.Fatalf("❌ Failed to reset workspace: %v", err)
	}
	
	fmt.Println("✅ Littleclaw workspace has been successfully reset!")
}

func main() {
	if len(os.Args) > 1 {
		if os.Args[1] == "configure" {
			runConfigure()
			return
		} else if os.Args[1] == "reset" {
			runReset()
			return
		}
	}

	fmt.Println("🦐 Starting Littleclaw Agent...")

	// 0. Try loading from Config File first
	cfg, err := config.Load()
	if err != nil {
		// Fallback to testing ENV variables so we don't break backward compatibility instantly
		if err := godotenv.Load(); err != nil {
			log.Println("⚠️ Could not load config.json or .env file.")
			log.Println("Please run: 'go run cmd/littleclaw/main.go configure'")
			log.Fatal(err)
		}
		log.Println("⚠️ Using Legacy .env configuration. Consider running 'littleclaw configure'.")
	}

	// 1. Setup Data Paths
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Cannot get home dir: %v", err)
	}
	workspace := filepath.Join(home, ".littleclaw", "workspace")

	// 2. Load Configuration
	var tgToken, tgAllowedUser, providerType, modelName, providerAPIKey string

	if cfg != nil {
		// Read from config.json
		tgToken = cfg.TelegramToken
		tgAllowedUser = cfg.TelegramAllowedUser
		providerType = cfg.ProviderType
		modelName = cfg.ProviderModel
		providerAPIKey = cfg.ProviderAPIKey
	} else {
		// Legacy .env fallback
		tgToken = os.Getenv("TELEGRAM_BOT_TOKEN")
		tgAllowedUser = os.Getenv("TELEGRAM_ALLOWED_USER_ID")
		providerType = os.Getenv("LLM_PROVIDER")
		if providerType == "" {
			providerType = "openrouter" // Default
		}

		if providerType == "ollama" {
			modelName = os.Getenv("OLLAMA_MODEL")
			if modelName == "" {
				modelName = "llama3.2" 
			}
		} else {
			providerAPIKey = os.Getenv("OPENROUTER_API_KEY")
			modelName = "gpt-4o-mini"
		}
	}

	if tgToken == "" {
		log.Println("⚠️ Missing Telegram Token! Please run 'go run cmd/littleclaw/main.go configure'")
		log.Fatal("Exiting due to missing configuration.")
	}

	var provider providers.Provider

	if providerType == "ollama" {
		log.Printf("🤖 Initializing Ollama provider with model: %s", modelName)
		provider = providers.NewOpenAIProvider(
			"ollama",
			"http://localhost:11434/v1", // Standard Ollama local port
			"ollama",                    // Dummy key
		)
	} else {
		if providerAPIKey == "" {
			log.Println("⚠️ Missing API keys! Please run 'go run cmd/littleclaw/main.go configure'")
			log.Fatal("Exiting due to missing configuration.")
		}
		
		log.Printf("🤖 Initializing %s provider", providerType)
		
		baseURL := "https://openrouter.ai/api/v1"
		if providerType == "openai" {
			baseURL = "https://api.openai.com/v1"
		}

		provider = providers.NewOpenAIProvider(
			providerType,
			baseURL,
			providerAPIKey,
		)
	}

	if tgToken == "" {
		log.Println("⚠️ Missing TELEGRAM_BOT_TOKEN. Export it to continue.")
		log.Fatal("Exiting due to missing configuration.")
	}

	allowedUsers := []string{}
	if tgAllowedUser != "" {
		allowedUsers = append(allowedUsers, tgAllowedUser)
	}

	// 3. Initialize Core Infrastructure
	msgBus := bus.NewMessageBus()

	// Initialize the NanoCore Agent Loop
	nanoCore, err := agent.NewNanoCore(provider, providerType, modelName, workspace, msgBus)
	if err != nil {
		log.Fatalf("Failed to initialize Agent Core: %v", err)
	}

	// Initialize the Telegram Channel
	tgChannel := telegram.NewChannel(tgToken, allowedUsers, msgBus)

	// Initialize Transcription Provider if configured
	if cfg != nil {
		if cfg.TranscriptionProvider == "groq" {
			log.Printf("🎙️ Initializing Groq transcription provider")
			groqTranscriber := providers.NewGroqTranscriptionProvider(cfg.TranscriptionAPIKey)
			tgChannel.SetTranscriptionProvider(groqTranscriber)
		} else if cfg.TranscriptionProvider == "openai" {
			log.Printf("🎙️ Initializing OpenAI/Local transcription provider")
			oaTranscriber := providers.NewOpenAITranscriptionProvider(cfg.TranscriptionBaseURL, cfg.TranscriptionAPIKey, cfg.TranscriptionModel)
			tgChannel.SetTranscriptionProvider(oaTranscriber)
		} else if cfg.TranscriptionProvider == "whisper-cli" {
			log.Printf("🎙️ Initializing Whisper CLI transcription provider")
			cliTranscriber := providers.NewWhisperCLITranscriptionProvider(cfg.TranscriptionModel)
			tgChannel.SetTranscriptionProvider(cliTranscriber)
		}
	}

	// Initialize the Background Heartbeat (Memory Janitor & Cron)
	// Setting interval to 30 seconds for easy testing. In production, this should be ~30 minutes.
	hb := agent.NewHeartbeat(nanoCore, 30*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 4. Start Background Heartbeat & Cron Service
	go hb.Start(ctx)
	nanoCore.StartCronService(ctx)
	log.Println("✅ Background Heartbeat & Cron daemon started.")

	// 5. Start Telegram Listener
	if err := tgChannel.Start(ctx); err != nil {
		log.Fatalf("Failed to start Telegram channel: %v", err)
	}
	log.Println("✅ Telegram channel started successfully. Listening for messages...")

	// 6. Start Message Processing Loop
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case inMsg := <-msgBus.Inbound:
				// Route inbound message to the NanoCore
				log.Printf("📩 Received message from %s (Chat: %s): %s", inMsg.SenderID, inMsg.ChatID, inMsg.Content)
				go nanoCore.RunAgentLoop(ctx, inMsg)

			case outMsg := <-msgBus.Outbound:
				// Route outbound message back to Telegram
				if outMsg.Channel == "telegram" {
					if err := tgChannel.SendMessage(ctx, outMsg.ChatID, outMsg.ReplyToMessageID, outMsg.Content, outMsg.Files); err != nil {
						log.Printf("❌ Failed to send Telegram message: %v", err)
					}
				}
			}
		}
	}()

	// Wait for termination signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down Littleclaw...")
	cancel()
}
