package telegram

import (
	"context"
	"fmt"
	"strconv"

	"littleclaw/pkg/bus"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Channel represents the Telegram integration
type Channel struct {
	bot       *tgbotapi.BotAPI
	bus       *bus.MessageBus
	token     string
	allowFrom map[string]bool // Set of allowed user IDs
}

// NewChannel creates a new Telegram channel
func NewChannel(token string, allowedUsers []string, messageBus *bus.MessageBus) *Channel {
	allowMap := make(map[string]bool)
	for _, u := range allowedUsers {
		allowMap[u] = true
	}
	return &Channel{
		token:     token,
		allowFrom: allowMap,
		bus:       messageBus,
	}
}

// Start connects to Telegram and begins listening for messages
func (t *Channel) Start(ctx context.Context) error {
	bot, err := tgbotapi.NewBotAPI(t.token)
	if err != nil {
		return fmt.Errorf("failed to init bot: %w", err)
	}
	t.bot = bot

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	go func() {
		for {
			select {
			case <-ctx.Done():
				t.bot.StopReceivingUpdates()
				return
			case update, ok := <-updates:
				if !ok {
					return
				}
				if update.Message == nil {
					continue
				}

				userID := strconv.FormatInt(update.Message.From.ID, 10)
				chatID := strconv.FormatInt(update.Message.Chat.ID, 10)

				// Security check: only process allowed users
				if len(t.allowFrom) > 0 && !t.allowFrom[userID] {
					continue
				}

				t.handleIncoming(update, userID, chatID)
			}
		}
	}()

	return nil
}

func (t *Channel) handleIncoming(update tgbotapi.Update, userID, chatID string) {
	text := update.Message.Text
	if update.Message.Caption != "" {
		text = update.Message.Caption
	}

	var mediaURLs []string
	
	// Handle photos (vision)
	if update.Message.Photo != nil && len(update.Message.Photo) > 0 {
		photos := update.Message.Photo
		largest := photos[len(photos)-1]
		fileURL, err := t.bot.GetFileDirectURL(largest.FileID)
		if err == nil {
			mediaURLs = append(mediaURLs, fileURL)
		}
	}

	// Wait on voice transcription handling (to be implemented with Groq/Whisper)
	// if update.Message.Voice != nil { ... }

	t.bus.SendInbound(bus.InboundMessage{
		Channel:  "telegram",
		SenderID: userID,
		ChatID:   chatID,
		Content:  text,
		Media:    mediaURLs,
	})
}

// SendMessage sends a response back to the Telegram chat
func (t *Channel) SendMessage(ctx context.Context, chatID, content string) error {
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid chat ID: %w", err)
	}

	msg := tgbotapi.NewMessage(id, content)
	_, err = t.bot.Send(msg)
	return err
}
