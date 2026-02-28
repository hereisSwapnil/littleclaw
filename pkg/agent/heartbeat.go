package agent

import (
	"context"
	"log"
	"time"

	"littleclaw/pkg/bus"
)

// Heartbeat runs a periodic background loop for the agent to perform
// autonomous tasks, mainly memory consolidation.
type Heartbeat struct {
	core     *NanoCore
	interval time.Duration
}

// NewHeartbeat creates a new background Heartbeat daemon.
func NewHeartbeat(core *NanoCore, interval time.Duration) *Heartbeat {
	return &Heartbeat{
		core:     core,
		interval: interval,
	}
}

// Start begins the heartbeat ticker. It blocks until ctx is canceled.
func (h *Heartbeat) Start(ctx context.Context) {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	// Initial check
	h.triggerConsolidation(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Println("Heartbeat stopping...")
			return
		case <-ticker.C:
			h.triggerConsolidation(ctx)
		}
	}
}

// triggerConsolidation pushes an internal message to the core to process memory.
func (h *Heartbeat) triggerConsolidation(ctx context.Context) {
	log.Println("💓 Heartbeat triggered: Initiating memory consolidation...")
	
	// Create a silent internal message to trigger the agent's memory reasoning.
	// In a real system you'd probably have a specific internal method for this, 
	// but routing an invisible message is an easy abstraction for now.
	internalMsg := bus.InboundMessage{
		Channel:  "internal", // Not telegram, so it shouldn't send back outbound messages
		SenderID: "system",
		ChatID:   "internal_memory",
		Content: `[SYSTEM CONSOLIDATION REQUEST]
Review the recent conversational history provided in your system prompt.
Extract any core facts, user preferences, projects, or entity relationships.
Use the 'update_core_memory' tool to update core facts.
Use the 'list_entities' and 'write_entity' tools to manage specific entity records.
You MUST be concise. Do not chat. Only use tools to read and write.`,
	}
	
	h.core.RunAgentLoop(ctx, internalMsg)
}
