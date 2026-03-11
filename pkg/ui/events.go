package ui

import (
	"encoding/json"
	"sync"
	"time"
)

// EventType categorizes events emitted by the agent.
type EventType string

const (
	// Agent loop events
	EventThinkingStart EventType = "thinking_start"
	EventThinkingEnd   EventType = "thinking_end"
	EventToolCall      EventType = "tool_call"
	EventToolResult    EventType = "tool_result"
	EventResponseReady EventType = "response_ready"

	// Message events
	EventMessageIn  EventType = "message_in"
	EventMessageOut EventType = "message_out"

	// Background events
	EventHeartbeat     EventType = "heartbeat"
	EventConsolidation EventType = "consolidation"
	EventCronFired     EventType = "cron_fired"
	EventCronCompleted EventType = "cron_completed"

	// Memory events
	EventMemoryWrite  EventType = "memory_write"
	EventEntityUpdate EventType = "entity_update"

	// Expression derived from state
	ExprIdle          Expression = "idle"
	ExprThinking      Expression = "thinking"
	ExprSpeaking      Expression = "speaking"
	ExprListening     Expression = "listening"
	ExprWorking       Expression = "working"
	ExprSearching     Expression = "searching"
	ExprRemembering   Expression = "remembering"
	ExprSleeping      Expression = "sleeping"
	ExprExcited       Expression = "excited"
	ExprConfused      Expression = "confused"
	ExprConsolidating Expression = "consolidating"
)

// Expression represents a facial expression state.
type Expression string

// Event is a single state-change event emitted by the agent.
type Event struct {
	Type      EventType   `json:"type"`
	Data      interface{} `json:"data,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}

// StateSnapshot represents the full current state of the agent for the UI.
type StateSnapshot struct {
	Expression    Expression `json:"expression"`
	Thought       string     `json:"thought"`
	Action        string     `json:"action"`
	ActionDetail  string     `json:"action_detail"`
	LastUserMsg   string     `json:"last_user_message"`
	LastBotMsg    string     `json:"last_bot_message"`
	UptimeSeconds int64      `json:"uptime_seconds"`
	Timestamp     time.Time  `json:"timestamp"`
}

// ActivityEntry is a single item in the activity timeline.
type ActivityEntry struct {
	Kind      string    `json:"kind"` // tool_call, message_in, message_out, cron, memory, heartbeat
	Title     string    `json:"title"`
	Detail    string    `json:"detail"`
	Timestamp time.Time `json:"timestamp"`
}

// StatsSnapshot contains periodic statistics.
type StatsSnapshot struct {
	ActiveCronJobs int       `json:"active_cron_jobs"`
	EntityCount    int       `json:"entity_count"`
	LastHeartbeat  time.Time `json:"last_heartbeat"`
	Uptime         int64     `json:"uptime_seconds"`
}

// WSMessage is the envelope sent over WebSocket to the frontend.
type WSMessage struct {
	Type string      `json:"type"` // "state", "activity", "stats"
	Data interface{} `json:"data"`
}

func (m WSMessage) JSON() []byte {
	data, _ := json.Marshal(m)
	return data
}

// EventBus is a publish/subscribe system for UI events.
type EventBus struct {
	mu          sync.RWMutex
	subscribers []chan Event
	history     []ActivityEntry
	maxHistory  int
}

// NewEventBus creates a new EventBus.
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make([]chan Event, 0),
		history:     make([]ActivityEntry, 0, 100),
		maxHistory:  100,
	}
}

// Subscribe returns a channel that will receive all events.
func (eb *EventBus) Subscribe() chan Event {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	ch := make(chan Event, 64)
	eb.subscribers = append(eb.subscribers, ch)
	return ch
}

// Unsubscribe removes a subscriber channel.
func (eb *EventBus) Unsubscribe(ch chan Event) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	for i, sub := range eb.subscribers {
		if sub == ch {
			eb.subscribers = append(eb.subscribers[:i], eb.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}

// Publish sends an event to all subscribers (non-blocking).
func (eb *EventBus) Publish(evt Event) {
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}
	eb.mu.RLock()
	for _, ch := range eb.subscribers {
		select {
		case ch <- evt:
		default:
			// Drop if subscriber is slow
		}
	}
	eb.mu.RUnlock()
}

// AddActivity appends an activity entry to the rolling history.
func (eb *EventBus) AddActivity(entry ActivityEntry) {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	eb.mu.Lock()
	eb.history = append(eb.history, entry)
	if len(eb.history) > eb.maxHistory {
		eb.history = eb.history[len(eb.history)-eb.maxHistory:]
	}
	eb.mu.Unlock()
}

// GetHistory returns the recent activity history.
func (eb *EventBus) GetHistory() []ActivityEntry {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	result := make([]ActivityEntry, len(eb.history))
	copy(result, eb.history)
	return result
}
