package ui

import (
	"strings"
	"sync"
	"time"
)

// StateTracker maintains the current state of the agent and
// translates raw events into facial expressions and UI state.
type StateTracker struct {
	mu sync.RWMutex

	eventBus  *EventBus
	startTime time.Time

	// Current state
	expression    Expression
	thought       string
	action        string
	actionDetail  string
	lastUserMsg   string
	lastBotMsg    string
	lastHeartbeat time.Time
	lastActivity  time.Time

	// Stats
	activeCronJobs int
	entityCount    int
}

// NewStateTracker creates a StateTracker wired to the given EventBus.
func NewStateTracker(eb *EventBus) *StateTracker {
	st := &StateTracker{
		eventBus:   eb,
		startTime:  time.Now(),
		expression: ExprIdle,
	}
	go st.listenAndDecay()
	return st
}

// HandleEvent processes a raw event and updates internal state.
func (st *StateTracker) HandleEvent(evt Event) {
	st.mu.Lock()
	defer st.mu.Unlock()

	st.lastActivity = time.Now()

	switch evt.Type {
	case EventThinkingStart:
		st.expression = ExprThinking
		st.thought = "Processing..."
		if data, ok := evt.Data.(map[string]interface{}); ok {
			if msg, ok := data["message"].(string); ok {
				st.thought = truncate(msg, 120)
			}
		}
		st.action = ""
		st.actionDetail = ""

	case EventToolCall:
		toolName := ""
		args := ""
		if data, ok := evt.Data.(map[string]interface{}); ok {
			toolName, _ = data["tool"].(string)
			args, _ = data["args"].(string)
		}
		st.action = toolName
		st.actionDetail = truncate(args, 200)
		st.expression = st.expressionForTool(toolName)
		st.thought = st.thoughtForTool(toolName, args)

	case EventToolResult:
		// Keep current expression briefly, it will decay to thinking

	case EventResponseReady:
		st.expression = ExprSpeaking
		if data, ok := evt.Data.(map[string]interface{}); ok {
			if msg, ok := data["message"].(string); ok {
				st.lastBotMsg = truncate(msg, 200)
				st.thought = truncate(msg, 80)
			}
		}
		st.action = ""
		st.actionDetail = ""

	case EventThinkingEnd:
		st.expression = ExprIdle
		st.thought = ""
		st.action = ""
		st.actionDetail = ""

	case EventMessageIn:
		st.expression = ExprListening
		if data, ok := evt.Data.(map[string]interface{}); ok {
			if msg, ok := data["message"].(string); ok {
				st.lastUserMsg = truncate(msg, 200)
				st.thought = "Listening..."
			}
		}

	case EventMessageOut:
		st.expression = ExprSpeaking
		if data, ok := evt.Data.(map[string]interface{}); ok {
			if msg, ok := data["message"].(string); ok {
				st.lastBotMsg = truncate(msg, 200)
			}
		}

	case EventHeartbeat:
		st.lastHeartbeat = time.Now()

	case EventConsolidation:
		st.expression = ExprConsolidating
		st.thought = "Consolidating memories..."

	case EventCronFired:
		st.expression = ExprWorking
		if data, ok := evt.Data.(map[string]interface{}); ok {
			if label, ok := data["label"].(string); ok {
				st.thought = "Running: " + label
			}
		}

	case EventCronCompleted:
		st.expression = ExprExcited
		if data, ok := evt.Data.(map[string]interface{}); ok {
			if status, ok := data["status"].(string); ok && status == "error" {
				st.expression = ExprConfused
			}
		}

	case EventMemoryWrite, EventEntityUpdate:
		st.expression = ExprRemembering
		st.thought = "Updating memory..."
		if data, ok := evt.Data.(map[string]interface{}); ok {
			if entity, ok := data["entity"].(string); ok {
				st.thought = "Remembering: " + entity
			}
		}
	}
}

// GetState returns a snapshot of the current state.
func (st *StateTracker) GetState() StateSnapshot {
	st.mu.RLock()
	defer st.mu.RUnlock()

	return StateSnapshot{
		Expression:    st.expression,
		Thought:       st.thought,
		Action:        st.action,
		ActionDetail:  st.actionDetail,
		LastUserMsg:   st.lastUserMsg,
		LastBotMsg:    st.lastBotMsg,
		UptimeSeconds: int64(time.Since(st.startTime).Seconds()),
		Timestamp:     time.Now(),
	}
}

// GetStats returns current statistics.
func (st *StateTracker) GetStats() StatsSnapshot {
	st.mu.RLock()
	defer st.mu.RUnlock()

	return StatsSnapshot{
		ActiveCronJobs: st.activeCronJobs,
		EntityCount:    st.entityCount,
		LastHeartbeat:  st.lastHeartbeat,
		Uptime:         int64(time.Since(st.startTime).Seconds()),
	}
}

// SetCronCount updates the active cron job count.
func (st *StateTracker) SetCronCount(n int) {
	st.mu.Lock()
	st.activeCronJobs = n
	st.mu.Unlock()
}

// SetEntityCount updates the entity count.
func (st *StateTracker) SetEntityCount(n int) {
	st.mu.Lock()
	st.entityCount = n
	st.mu.Unlock()
}

// listenAndDecay runs in the background, decaying transient expressions back to idle.
func (st *StateTracker) listenAndDecay() {
	ticker := time.NewTicker(3 * time.Second)
	sleepTicker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	defer sleepTicker.Stop()

	for {
		select {
		case <-ticker.C:
			st.mu.Lock()
			// Decay speaking/excited/confused back to idle after 3s of no activity
			if st.expression == ExprSpeaking || st.expression == ExprExcited ||
				st.expression == ExprConfused || st.expression == ExprRemembering ||
				st.expression == ExprConsolidating || st.expression == ExprListening {
				if time.Since(st.lastActivity) > 3*time.Second {
					st.expression = ExprIdle
					st.thought = ""
					st.action = ""
					st.actionDetail = ""
				}
			}
			st.mu.Unlock()

		case <-sleepTicker.C:
			st.mu.Lock()
			// Go to sleep after 5 minutes of inactivity
			if st.expression == ExprIdle && time.Since(st.lastActivity) > 5*time.Minute {
				st.expression = ExprSleeping
				st.thought = "zzz..."
			}
			st.mu.Unlock()
		}
	}
}

// expressionForTool maps a tool name to a facial expression.
func (st *StateTracker) expressionForTool(toolName string) Expression {
	switch {
	case toolName == "web_search" || toolName == "web_fetch":
		return ExprSearching
	case toolName == "update_core_memory" || toolName == "write_entity" || toolName == "read_entity":
		return ExprRemembering
	case toolName == "exec":
		return ExprWorking
	case strings.HasPrefix(toolName, "add_cron") || strings.HasPrefix(toolName, "remove_cron"):
		return ExprWorking
	default:
		return ExprWorking
	}
}

// thoughtForTool generates a human-readable thought based on the tool being used.
func (st *StateTracker) thoughtForTool(toolName, args string) string {
	switch toolName {
	case "web_search":
		return "Searching the web..."
	case "web_fetch":
		return "Fetching a webpage..."
	case "exec":
		return "Running a command..."
	case "read_file":
		return "Reading a file..."
	case "write_file":
		return "Writing a file..."
	case "update_core_memory":
		return "Updating my memory..."
	case "write_entity":
		return "Saving entity info..."
	case "read_entity":
		return "Looking up an entity..."
	case "list_entities":
		return "Checking known entities..."
	case "add_cron":
		return "Scheduling a task..."
	case "remove_cron":
		return "Removing a scheduled task..."
	case "list_cron":
		return "Checking scheduled tasks..."
	case "send_telegram_file":
		return "Sending a file..."
	default:
		return "Using tool: " + toolName
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
