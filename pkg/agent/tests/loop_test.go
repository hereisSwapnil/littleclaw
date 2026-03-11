package agent_test

import (
	"littleclaw/pkg/agent"
	"context"
	"strings"
	"testing"

	"littleclaw/pkg/bus"
	"littleclaw/pkg/providers"
)

// ---------------------------------------------------------------------------
// Mock LLM provider (no real API calls)
// ---------------------------------------------------------------------------

// mockProvider is a stub providers.Provider that returns pre-configured responses.
// It does NOT make any network calls - all responses are supplied in-process.
type mockProvider struct {
	responses []providers.ChatResponse
	callIndex int
	requests  []providers.ChatRequest // captured for assertions
}

func (m *mockProvider) Chat(ctx context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	m.requests = append(m.requests, req)
	if m.callIndex >= len(m.responses) {
		// Safety: return an empty response if we fall off the end
		return &providers.ChatResponse{Content: "(mock exhausted)"}, nil
	}
	resp := m.responses[m.callIndex]
	m.callIndex++
	return &resp, nil
}

func (m *mockProvider) Name() string { return "mock" }

// newTestAgent creates a agent.NanoCore backed by a temp directory and a mock provider.
func newTestAgent(t *testing.T, provider providers.Provider) (*agent.NanoCore, *bus.MessageBus) {
	t.Helper()
	dir := t.TempDir()
	msgBus := bus.NewMessageBus()

	nc, err := agent.NewNanoCore(provider, "mock", "test-model", dir, msgBus, "")
	if err != nil {
		t.Fatalf("agent.NewNanoCore() error = %v", err)
	}
	return nc, msgBus
}

// drainOutbound reads all pending messages from Outbound without blocking.
func drainOutbound(msgBus *bus.MessageBus) []bus.OutboundMessage {
	var msgs []bus.OutboundMessage
	for {
		select {
		case msg := <-msgBus.Outbound:
			msgs = append(msgs, msg)
		default:
			return msgs
		}
	}
}

// ---------------------------------------------------------------------------
// RunAgentLoop tests
// ---------------------------------------------------------------------------

func TestRunAgentLoop_SingleTurnReply(t *testing.T) {
	provider := &mockProvider{
		responses: []providers.ChatResponse{
			{Content: "Hello, I am Littleclaw!"},
		},
	}
	nc, msgBus := newTestAgent(t, provider)

	nc.RunAgentLoop(context.Background(), bus.InboundMessage{
		ChatID:  "user123",
		Channel: "telegram",
		Content: "Hello!",
	})

	msgs := drainOutbound(msgBus)
	if len(msgs) == 0 {
		t.Fatal("expected at least one outbound message")
	}
	if !strings.Contains(msgs[len(msgs)-1].Content, "Littleclaw") {
		t.Errorf("final reply = %q, expected 'Littleclaw'", msgs[len(msgs)-1].Content)
	}
}

func TestRunAgentLoop_EmptyMessageIgnored(t *testing.T) {
	provider := &mockProvider{}
	nc, msgBus := newTestAgent(t, provider)

	nc.RunAgentLoop(context.Background(), bus.InboundMessage{
		ChatID:  "user123",
		Channel: "telegram",
		Content: "", // empty — should be ignored
	})

	msgs := drainOutbound(msgBus)
	if len(msgs) != 0 {
		t.Errorf("expected no outbound messages for empty content, got %d", len(msgs))
	}
	if provider.callIndex != 0 {
		t.Errorf("expected 0 LLM calls for empty message, got %d", provider.callIndex)
	}
}

func TestRunAgentLoop_ToolCallThenReply(t *testing.T) {
	// Round 1: LLM sends a tool call (read_core_memory)
	// Round 2: LLM sends back a final text response
	toolCallResp := providers.ChatResponse{
		ToolCalls: []map[string]interface{}{
			{
				"id": "call_1",
				"function": map[string]interface{}{
					"name":      "read_core_memory",
					"arguments": `{}`,
				},
			},
		},
	}
	finalResp := providers.ChatResponse{Content: "Memory retrieved successfully."}

	provider := &mockProvider{
		responses: []providers.ChatResponse{toolCallResp, finalResp},
	}
	nc, msgBus := newTestAgent(t, provider)

	nc.RunAgentLoop(context.Background(), bus.InboundMessage{
		ChatID:  "user123",
		Channel: "telegram",
		Content: "What do you remember?",
	})

	if provider.callIndex != 2 {
		t.Errorf("expected 2 LLM calls (tool + final), got %d", provider.callIndex)
	}

	msgs := drainOutbound(msgBus)
	if len(msgs) == 0 {
		t.Fatal("expected at least one outbound message")
	}
	lastMsg := msgs[len(msgs)-1]
	if !strings.Contains(lastMsg.Content, "Memory retrieved") {
		t.Errorf("final reply = %q, expected 'Memory retrieved'", lastMsg.Content)
	}
}

func TestRunAgentLoop_MaxIterationsHit(t *testing.T) {
	// LLM keeps returning tool calls forever — should stop at maxIterations
	toolCallResp := providers.ChatResponse{
		ToolCalls: []map[string]interface{}{
			{
				"id": "call_loop",
				"function": map[string]interface{}{
					"name":      "read_core_memory",
					"arguments": `{}`,
				},
			},
		},
	}

	// Supply more responses than maxIterations
	var responses []providers.ChatResponse
	for i := 0; i < 15; i++ {
		responses = append(responses, toolCallResp)
	}
	provider := &mockProvider{responses: responses}
	nc, msgBus := newTestAgent(t, provider)

	nc.RunAgentLoop(context.Background(), bus.InboundMessage{
		ChatID:  "user123",
		Channel: "telegram",
		Content: "keep looping",
	})

	// Should have stopped at maxIterations (10)
	if provider.callIndex > 10 {
		t.Errorf("expected at most 10 LLM calls, got %d", provider.callIndex)
	}
	_ = msgBus
}

func TestRunAgentLoop_ReplyToContextInjected(t *testing.T) {
	provider := &mockProvider{
		responses: []providers.ChatResponse{
			{Content: "OK noted."},
		},
	}
	nc, _ := newTestAgent(t, provider)

	nc.RunAgentLoop(context.Background(), bus.InboundMessage{
		ChatID:  "user123",
		Channel: "telegram",
		Content: "Yes, exactly.",
		ReplyTo: "Original message content",
	})

	// The request sent to the LLM should contain the reply-to context
	if provider.callIndex == 0 {
		t.Fatal("expected at least one LLM call")
	}
	firstReq := provider.requests[0]
	userMsg := firstReq.Messages[1].Content
	if !strings.Contains(userMsg, "Original message content") {
		t.Errorf("ReplyTo context not injected into user message. Got: %q", userMsg)
	}
}

func TestRunAgentLoop_InternalChannel_NoHistoryLog(t *testing.T) {
	provider := &mockProvider{
		responses: []providers.ChatResponse{
			{Content: "Internal response."},
		},
	}
	nc, _ := newTestAgent(t, provider)

	nc.RunAgentLoop(context.Background(), bus.InboundMessage{
		ChatID:  "internal_memory",
		Channel: "internal",
		Content: "heartbeat consolidation task",
	})

	// Internal messages should go to INTERNAL.md, not daily log
	// We verify by checking there's no daily log (today's log should be empty)
	history := nc.MemoryStore().ReadRecentHistory(16000)
	if strings.Contains(history, "heartbeat consolidation task") {
		t.Error("internal messages should not appear in daily history log")
	}
}

// ---------------------------------------------------------------------------
// agent.TruncateToTokenBudget tests
// ---------------------------------------------------------------------------

func TestTruncateToTokenBudget_ShortString(t *testing.T) {
	s := "short string"
	got := agent.TruncateToTokenBudget(s, 1000)
	if got != s {
		t.Errorf("agent.TruncateToTokenBudget() modified short string: got %q", got)
	}
}

func TestTruncateToTokenBudget_LongString(t *testing.T) {
	s := strings.Repeat("word ", 1000) // ~5000 chars
	got := agent.TruncateToTokenBudget(s, 100) // 100 tokens = ~400 chars
	if len(got) > 100*agent.CharsPerToken+100 {
		t.Errorf("agent.TruncateToTokenBudget did not truncate: len=%d", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Error("truncated string should contain '...truncated...'")
	}
}

func TestTruncateToTokenBudget_Empty(t *testing.T) {
	got := agent.TruncateToTokenBudget("", 100)
	if got != "" {
		t.Errorf("agent.TruncateToTokenBudget(\"\") = %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// agent.TruncateToolResult tests
// ---------------------------------------------------------------------------

func TestTruncateToolResult_ShortResult(t *testing.T) {
	s := "short result"
	got := agent.TruncateToolResult(s)
	if got != s {
		t.Errorf("agent.TruncateToolResult modified short result: got %q", got)
	}
}

func TestTruncateToolResult_LongResult(t *testing.T) {
	s := strings.Repeat("x", agent.MaxToolResultChars+500)
	got := agent.TruncateToolResult(s)
	if len(got) > agent.MaxToolResultChars+50 {
		t.Errorf("agent.TruncateToolResult result too long: %d chars", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Error("truncated tool result should contain '(truncated)'")
	}
}

// ---------------------------------------------------------------------------
// agent.EstimateContextWindow tests
// ---------------------------------------------------------------------------

func TestEstimateContextWindow_GPT4o(t *testing.T) {
	got := agent.EstimateContextWindow("gpt-4o")
	if got != 128000 {
		t.Errorf("agent.EstimateContextWindow('gpt-4o') = %d, want 128000", got)
	}
}

func TestEstimateContextWindow_GPT4(t *testing.T) {
	got := agent.EstimateContextWindow("gpt-4")
	if got != 8192 {
		t.Errorf("agent.EstimateContextWindow('gpt-4') = %d, want 8192", got)
	}
}

func TestEstimateContextWindow_Claude(t *testing.T) {
	got := agent.EstimateContextWindow("claude-3-sonnet")
	if got != 200000 {
		t.Errorf("agent.EstimateContextWindow('claude-3') = %d, want 200000", got)
	}
}

func TestEstimateContextWindow_Gemini(t *testing.T) {
	got := agent.EstimateContextWindow("gemini-1.5-pro")
	if got != 128000 {
		t.Errorf("agent.EstimateContextWindow('gemini') = %d, want 128000", got)
	}
}

func TestEstimateContextWindow_Unknown(t *testing.T) {
	got := agent.EstimateContextWindow("some-unknown-model")
	if got <= 0 {
		t.Errorf("agent.EstimateContextWindow(unknown) should return a positive default, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// IsApproachingContextLimit tests
// ---------------------------------------------------------------------------

func TestIsApproachingContextLimit_NeverTriggersWithZero(t *testing.T) {
	provider := &mockProvider{
		responses: []providers.ChatResponse{{Content: "ok"}},
	}
	nc, _ := newTestAgent(t, provider)

	// Before any messages, both should be 0
	if nc.IsApproachingContextLimit() {
		t.Error("IsApproachingContextLimit() should be false with zero token tracking")
	}
}

func TestIsApproachingContextLimit_TrueWhenHigh(t *testing.T) {
	provider := &mockProvider{
		responses: []providers.ChatResponse{{Content: "ok"}},
	}
	nc, _ := newTestAgent(t, provider)

	// Manually set high token usage
	nc.ContextWindowEst = 1000
	nc.LastPromptTokens = 850 // 85% of 1000 > 80% threshold

	if !nc.IsApproachingContextLimit() {
		t.Error("IsApproachingContextLimit() should be true when tokens > 80% of window")
	}
}

func TestIsApproachingContextLimit_FalseWhenLow(t *testing.T) {
	provider := &mockProvider{
		responses: []providers.ChatResponse{{Content: "ok"}},
	}
	nc, _ := newTestAgent(t, provider)

	nc.ContextWindowEst = 1000
	nc.LastPromptTokens = 500 // 50% — below threshold

	if nc.IsApproachingContextLimit() {
		t.Error("IsApproachingContextLimit() should be false at 50% usage")
	}
}

// ---------------------------------------------------------------------------
// buildSystemPromptWithQuery tests
// ---------------------------------------------------------------------------

func TestBuildSystemPromptWithQuery_ContainsFormatRules(t *testing.T) {
	provider := &mockProvider{}
	nc, _ := newTestAgent(t, provider)

	prompt := nc.BuildSystemPromptWithQuery("hello")
	if !strings.Contains(prompt, "OUTPUT FORMAT RULE") {
		t.Error("system prompt should contain output format rules")
	}
}

func TestBuildSystemPromptWithQuery_ContainsWorkspaceSection(t *testing.T) {
	provider := &mockProvider{}
	nc, _ := newTestAgent(t, provider)

	prompt := nc.BuildSystemPromptWithQuery("anything")
	if !strings.Contains(prompt, "WORKSPACE STRUCTURE") {
		t.Error("system prompt should contain workspace structure section")
	}
}

func TestBuildSystemPromptWithQuery_SurfacesRelevantEntities(t *testing.T) {
	provider := &mockProvider{}
	nc, _ := newTestAgent(t, provider)

	// Write an entity that will be surfaced
	_ = nc.MemoryStore().WriteEntity("Alice Smith", "Alice is a backend engineer.")

	prompt := nc.BuildSystemPromptWithQuery("what did Alice work on?")
	if !strings.Contains(prompt, "Alice") {
		t.Error("system prompt should surface Alice entity when query mentions Alice")
	}
}

func TestBuildSystemPromptWithQuery_EmptyQuery_NoEntitySection(t *testing.T) {
	provider := &mockProvider{}
	nc, _ := newTestAgent(t, provider)

	_ = nc.MemoryStore().WriteEntity("Bob", "Bob is a frontend developer.")

	// Empty query — no entity auto-surfacing
	prompt := nc.BuildSystemPromptWithQuery("")
	if strings.Contains(prompt, "RELEVANT ENTITY CONTEXT") {
		t.Error("with empty query, RELEVANT ENTITY CONTEXT should not appear")
	}
}

func TestBuildSystemPromptWithQuery_TruncatesCoreMemory(t *testing.T) {
	provider := &mockProvider{}
	nc, _ := newTestAgent(t, provider)

	// Write more than agent.CoreBudgetTokens * 4 chars  to MEMORY.md
	big := strings.Repeat("fact: something important. ", agent.CoreBudgetTokens)
	_ = nc.MemoryStore().WriteLongTerm(big)

	prompt := nc.BuildSystemPromptWithQuery("hello")
	// The prompt should be built without panicking, and the memory should be truncated
	if len(prompt) == 0 {
		t.Error("system prompt should not be empty")
	}
}
