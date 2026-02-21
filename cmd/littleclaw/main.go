package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"littleclaw/pkg/agent"
	"littleclaw/pkg/bus"
	"littleclaw/pkg/channels/telegram"
	"littleclaw/pkg/providers"
)

func main() {
	fmt.Println("🦐 Starting Littleclaw Agent...")

	// 1. Setup Data Paths
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Cannot get home dir: %v", err)
	}
	workspace := filepath.Join(home, ".littleclaw", "workspace")

	// 2. Load Configuration (Simulated via ENV for Phase 1)
	tgToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	tgAllowedUser := os.Getenv("TELEGRAM_ALLOWED_USER_ID")
	openRouterKey := os.Getenv("OPENROUTER_API_KEY")

	if tgToken == "" || openRouterKey == "" {
		log.Println("⚠️ Missing API keys! Please set TELEGRAM_BOT_TOKEN and OPENROUTER_API_KEY.")
		log.Println("Run: export TELEGRAM_BOT_TOKEN='...' && export OPENROUTER_API_KEY='...'")
		log.Fatal("Exiting due to missing configuration.")
	}

	allowedUsers := []string{}
	if tgAllowedUser != "" {
		allowedUsers = append(allowedUsers, tgAllowedUser)
	}

	// 3. Initialize Core Infrastructure
	msgBus := bus.NewMessageBus()

	// Initialize Provider (Using OpenRouter via OpenAI Compatibility)
	provider := providers.NewOpenAIProvider(
		"openrouter",
		"https://openrouter.ai/api/v1",
		openRouterKey,
	)

	// Initialize the NanoCore Agent Loop
	nanoCore, err := agent.NewNanoCore(provider, workspace, msgBus)
	if err != nil {
		log.Fatalf("Failed to initialize Agent Core: %v", err)
	}

	// Initialize the Telegram Channel
	tgChannel := telegram.NewChannel(tgToken, allowedUsers, msgBus)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 4. Start Telegram Listener
	if err := tgChannel.Start(ctx); err != nil {
		log.Fatalf("Failed to start Telegram channel: %v", err)
	}
	log.Println("✅ Telegram channel started successfully. Listening for messages...")

	// 5. Start Message Processing Loop
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
					if err := tgChannel.SendMessage(ctx, outMsg.ChatID, outMsg.Content); err != nil {
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
