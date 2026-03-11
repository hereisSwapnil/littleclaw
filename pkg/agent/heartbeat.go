package agent

import (
	"context"
	"fmt"
	"log"
	"time"

	"littleclaw/pkg/bus"
)

// Heartbeat runs a periodic background loop for the agent to perform
// autonomous tasks, mainly memory consolidation and summarization.
type Heartbeat struct {
	core     *NanoCore
	interval time.Duration

	// Exported fields for external test inspection.
	Core     *NanoCore
	Interval time.Duration
}

// NewHeartbeat creates a new background Heartbeat daemon.
func NewHeartbeat(core *NanoCore, interval time.Duration) *Heartbeat {
	return &Heartbeat{
		core:     core,
		interval: interval,
		Core:     core,
		Interval: interval,
	}
}

// Start begins the heartbeat ticker. It blocks until ctx is canceled.
func (h *Heartbeat) Start(ctx context.Context) {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	// Initial check
	h.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Println("Heartbeat stopping...")
			return
		case <-ticker.C:
			h.tick(ctx)
		}
	}
}

// tick runs all heartbeat tasks: consolidation, summarization, and pre-compaction check.
func (h *Heartbeat) tick(ctx context.Context) {
	h.triggerSummarization(ctx)
	h.triggerConsolidation(ctx)
	h.checkPreCompaction(ctx)
}

// triggerConsolidation pushes an internal message to the core to process memory.
// It only runs if new history has been appended since the last consolidation.
func (h *Heartbeat) triggerConsolidation(ctx context.Context) {
	// Only consolidate if there is actually new content to process
	if !h.core.memoryStore.IsDirtyAndClear() {
		log.Println("💤 Heartbeat: No new history since last consolidation, skipping.")
		return
	}

	log.Println("💓 Heartbeat triggered: Initiating memory consolidation...")

	internalMsg := bus.InboundMessage{
		Channel:  "internal",
		SenderID: "system",
		ChatID:   "internal_memory",
		Content: `[SYSTEM CONSOLIDATION REQUEST]
Review the recent conversational history provided in your system prompt.
Extract any core facts, user preferences, projects, or entity relationships that should be remembered long-term.

RULES:
1. Use 'append_core_memory' for NEW facts that aren't already in core memory.
2. Only use 'update_core_memory' if core memory needs reorganizing or deduplication — and ALWAYS 'read_core_memory' first.
3. Use 'list_entities' to check existing entities, then 'write_entity' for detailed knowledge about specific people, projects, or topics.
4. Do NOT duplicate information that already exists in core memory.
5. Be concise. Do not chat. Only use tools to read and write memory.`,
	}

	h.core.RunAgentLoop(ctx, internalMsg)
}

// triggerSummarization checks if yesterday's daily log needs summarization and triggers it.
func (h *Heartbeat) triggerSummarization(ctx context.Context) {
	needsSummary, date, content := h.core.memoryStore.NeedsSummarization()
	if !needsSummary {
		return
	}

	log.Printf("📝 Heartbeat: Yesterday's log (%s) exceeds threshold, triggering summarization...", date)

	internalMsg := bus.InboundMessage{
		Channel:  "internal",
		SenderID: "system",
		ChatID:   "internal_memory",
		Content: fmt.Sprintf(`[SYSTEM SUMMARIZATION REQUEST]
The conversation log for %s is too large to include in full context. Summarize it into a concise digest.

RULES:
1. Capture the KEY topics discussed, decisions made, and important facts mentioned.
2. Preserve any action items, promises, or commitments.
3. Keep entity names and project references intact.
4. The summary should be 200-500 words maximum.
5. Write the summary using the write_summary tool with date="%s" (IMPORTANT: use this exact date).
6. Do NOT chat. Only produce the summary.

FULL LOG FOR %s:
%s`, date, date, date, content),
	}

	h.core.RunAgentLoop(ctx, internalMsg)
}

// checkPreCompaction triggers an early consolidation if the agent is approaching context limits.
func (h *Heartbeat) checkPreCompaction(ctx context.Context) {
	if !h.core.IsApproachingContextLimit() {
		return
	}

	log.Println("⚡ Heartbeat: Context window pressure detected, triggering pre-compaction flush...")

	internalMsg := bus.InboundMessage{
		Channel:  "internal",
		SenderID: "system",
		ChatID:   "internal_memory",
		Content: `[SYSTEM PRE-COMPACTION FLUSH]
Context window is filling up. Capture any durable memories to disk NOW before they are lost.

RULES:
1. Read core memory first with 'read_core_memory'.
2. Append any new important facts with 'append_core_memory'.
3. If core memory is bloated or has duplicates, use 'update_core_memory' to reorganize it.
4. Check entities with 'list_entities' and update any that have new information.
5. Be aggressive about saving — this may be the last chance before context is trimmed.
6. Do NOT chat. Only use tools.`,
	}

	h.core.RunAgentLoop(ctx, internalMsg)
}

// Exported wrappers for external test access.

// TriggerConsolidation is the exported equivalent of triggerConsolidation.
func (h *Heartbeat) TriggerConsolidation(ctx context.Context) { h.triggerConsolidation(ctx) }

// TriggerSummarization is the exported equivalent of triggerSummarization.
func (h *Heartbeat) TriggerSummarization(ctx context.Context) { h.triggerSummarization(ctx) }

// CheckPreCompaction is the exported equivalent of checkPreCompaction.
func (h *Heartbeat) CheckPreCompaction(ctx context.Context) { h.checkPreCompaction(ctx) }

// Tick runs one full heartbeat cycle (exported for tests).
func (h *Heartbeat) Tick(ctx context.Context) { h.tick(ctx) }
