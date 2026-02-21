package bus

// InboundMessage represents a message received from a channel (e.g., Telegram)
type InboundMessage struct {
	Channel  string
	SenderID string
	ChatID   string
	Content  string
	Media    []string // URLs or local paths to media
}

// OutboundMessage represents a message to be sent to a channel
type OutboundMessage struct {
	Channel string
	ChatID  string
	Content string
}

// MessageBus routes messages between channels and the agent core
type MessageBus struct {
	Inbound  chan InboundMessage
	Outbound chan OutboundMessage
}

// NewMessageBus creates a new initialized MessageBus
func NewMessageBus() *MessageBus {
	return &MessageBus{
		Inbound:  make(chan InboundMessage, 100),
		Outbound: make(chan OutboundMessage, 100),
	}
}

func (b *MessageBus) SendInbound(msg InboundMessage) {
	b.Inbound <- msg
}

func (b *MessageBus) SendOutbound(msg OutboundMessage) {
	b.Outbound <- msg
}
