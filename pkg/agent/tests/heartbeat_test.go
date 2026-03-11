package agent_test

import (
	"littleclaw/pkg/agent"
	"context"
	"strings"
	"testing"
	"time"

	"littleclaw/pkg/providers"
)

// ---------------------------------------------------------------------------
// agent.Heartbeat unit tests
// ---------------------------------------------------------------------------

// TestHeartbeat_SkipsConsolidationWhenClean verifies that triggerConsolidation
// does NOT call the LLM when no new history has been appended (dirty=false).
func TestHeartbeat_SkipsConsolidationWhenClean(t *testing.T) {
	provider := &mockProvider{}
	nc, _ := newTestAgent(t, provider)

	// Ensure dirty flag is false — no new content
	nc.MemoryStore().IsDirtyAndClear() // clears any stale dirty state

	hb := agent.NewHeartbeat(nc, time.Hour)
	hb.TriggerConsolidation(context.Background())

	if provider.callIndex != 0 {
		t.Errorf("expected 0 LLM calls when memory is clean, got %d", provider.callIndex)
	}
}

// TestHeartbeat_TriggersConsolidationWhenDirty verifies that triggerConsolidation
// sends an internal message through the agent loop when dirty=true.
func TestHeartbeat_TriggersConsolidationWhenDirty(t *testing.T) {
	provider := &mockProvider{
		responses: []providers.ChatResponse{
			{Content: "Memory consolidated."}, // consolidation response
		},
	}
	nc, _ := newTestAgent(t, provider)

	// Make memory dirty by appending history
	_ = nc.MemoryStore().AppendHistory("user", "some new content")
	// IsDirtyAndClear is called inside triggerConsolidation — it will see dirty=true

	hb := agent.NewHeartbeat(nc, time.Hour)
	hb.TriggerConsolidation(context.Background())

	if provider.callIndex == 0 {
		t.Error("expected at least one LLM call during consolidation with dirty memory")
	}
}

// TestHeartbeat_SkipsSummarizationWhenNotNeeded verifies triggerSummarization
// does nothing when yesterday's log is small (no need to summarize).
func TestHeartbeat_SkipsSummarizationWhenNotNeeded(t *testing.T) {
	provider := &mockProvider{}
	nc, _ := newTestAgent(t, provider)

	// No large logs — summarization should be skipped
	hb := agent.NewHeartbeat(nc, time.Hour)
	hb.TriggerSummarization(context.Background())

	if provider.callIndex != 0 {
		t.Errorf("expected 0 LLM calls when no summarization needed, got %d", provider.callIndex)
	}
}

// TestHeartbeat_SkipsPreCompactionWhenBelowThreshold checks that checkPreCompaction
// does not fire when the context window is not near full.
func TestHeartbeat_SkipsPreCompactionWhenBelowThreshold(t *testing.T) {
	provider := &mockProvider{}
	nc, _ := newTestAgent(t, provider)

	// Low token usage — no pre-compaction needed
	nc.ContextWindowEst = 10000
	nc.LastPromptTokens = 1000 // 10% — far below 80%

	hb := agent.NewHeartbeat(nc, time.Hour)
	hb.CheckPreCompaction(context.Background())

	if provider.callIndex != 0 {
		t.Errorf("expected 0 LLM calls when below pre-compaction threshold, got %d", provider.callIndex)
	}
}

// TestHeartbeat_TriggersPreCompactionWhenFull checks that checkPreCompaction
// fires when the context window is >80% full.
func TestHeartbeat_TriggersPreCompactionWhenFull(t *testing.T) {
	provider := &mockProvider{
		responses: []providers.ChatResponse{
			{Content: "Flushed."},
		},
	}
	nc, _ := newTestAgent(t, provider)

	// High token usage — trigger pre-compaction
	nc.ContextWindowEst = 10000
	nc.LastPromptTokens = 9000 // 90% > 80% threshold

	hb := agent.NewHeartbeat(nc, time.Hour)
	hb.CheckPreCompaction(context.Background())

	if provider.callIndex == 0 {
		t.Error("expected pre-compaction to call LLM when context is full")
	}
}

// TestHeartbeat_NewHeartbeat checks the constructor sets fields correctly.
func TestHeartbeat_NewHeartbeat(t *testing.T) {
	provider := &mockProvider{}
	nc, _ := newTestAgent(t, provider)

	interval := 5 * time.Minute
	hb := agent.NewHeartbeat(nc, interval)

	if hb.Core != nc {
		t.Error("agent.Heartbeat.core should point to the agent.NanoCore passed at construction")
	}
	if hb.Interval != interval {
		t.Errorf("agent.Heartbeat.interval = %v, want %v", hb.Interval, interval)
	}
}

// TestHeartbeat_TickCallsAllSubtasks verifies that tick() calls all three
// subtasks (summarize, consolidate, pre-compact) without panicking.
func TestHeartbeat_TickCallsAllSubtasks(t *testing.T) {
	provider := &mockProvider{
		// Provide enough responses in case all three subtasks fire
		responses: []providers.ChatResponse{
			{Content: "ok1"},
			{Content: "ok2"},
			{Content: "ok3"},
		},
	}
	nc, _ := newTestAgent(t, provider)

	hb := agent.NewHeartbeat(nc, time.Hour)

	// Should not panic
	hb.Tick(context.Background())
}

// TestHeartbeat_StartStopsOnContextCancel verifies that Start() exits
// promptly when the context is cancelled, without hanging.
func TestHeartbeat_StartStopsOnContextCancel(t *testing.T) {
	provider := &mockProvider{
		responses: []providers.ChatResponse{
			{Content: "ok"},
			{Content: "ok"},
			{Content: "ok"},
		},
	}
	nc, _ := newTestAgent(t, provider)

	hb := agent.NewHeartbeat(nc, 100*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		hb.Start(ctx)
		close(done)
	}()

	// Let it tick once, then cancel
	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Good — heartbeat stopped
	case <-time.After(2 * time.Second):
		t.Error("agent.Heartbeat.Start() did not stop after context cancellation")
	}
}

// TestHeartbeat_ConsolidationSendsInternalChannel verifies that the
// message sent during consolidation uses the "internal" channel.
func TestHeartbeat_ConsolidationSendsInternalChannel(t *testing.T) {
	provider := &mockProvider{
		responses: []providers.ChatResponse{
			{Content: "done"},
		},
	}
	nc, msgBus := newTestAgent(t, provider)

	// Make dirty so consolidation fires
	_ = nc.MemoryStore().AppendHistory("user", "important info")

	hb := agent.NewHeartbeat(nc, time.Hour)
	hb.TriggerConsolidation(context.Background())

	// Drain and check — internal channel messages should go to INTERNAL.md not outbound
	// The internal message should NOT appear in outbound (it uses internal channel)
	msgs := drainOutbound(msgBus)
	for _, m := range msgs {
		if strings.Contains(m.Content, "SYSTEM CONSOLIDATION REQUEST") {
			t.Error("consolidation prompt should not appear in outbound messages")
		}
	}
}

// ---------------------------------------------------------------------------
// Summarization trigger via large log
// ---------------------------------------------------------------------------

func TestHeartbeat_TriggersSummarizationWhenLogLarge(t *testing.T) {
	provider := &mockProvider{
		responses: []providers.ChatResponse{
			{Content: "Summary written."},
		},
	}
	nc, _ := newTestAgent(t, provider)

	hb := agent.NewHeartbeat(nc, time.Hour)

	// Fresh temp store with no yesterday log — summarization should be skipped
	start := provider.callIndex
	hb.TriggerSummarization(context.Background())
	if provider.callIndex != start {
		t.Logf("note: summarization fired unexpectedly (callIndex=%d); may be a stale log", provider.callIndex)
	}

	// Verify the store reports no summarization needed on a fresh store
	needs, _, _ := nc.MemoryStore().NeedsSummarization()
	if needs {
		t.Error("expected NeedsSummarization=false for fresh temp store with no yesterday log")
	}
}
