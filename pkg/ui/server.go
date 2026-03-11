package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

// Server serves the face UI frontend and WebSocket state stream.
type Server struct {
	eventBus     *EventBus
	stateTracker *StateTracker
	port         int
	staticFS     fs.FS // embedded frontend files
	wsClients    map[*websocket.Conn]context.CancelFunc
	wsMu         sync.Mutex
}

// NewServer creates a new UI server.
func NewServer(eb *EventBus, st *StateTracker, port int, staticFS fs.FS) *Server {
	return &Server{
		eventBus:     eb,
		stateTracker: st,
		port:         port,
		staticFS:     staticFS,
		wsClients:    make(map[*websocket.Conn]context.CancelFunc),
	}
}

// Start launches the HTTP server in a goroutine. It blocks until ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Static files (the face UI SPA)
	mux.Handle("/", http.FileServer(http.FS(s.staticFS)))

	// WebSocket endpoint for live state streaming
	mux.HandleFunc("/ws", s.handleWebSocket)

	// REST API endpoints
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/history", s.handleHistory)
	mux.HandleFunc("/api/stats", s.handleStats)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
	}

	// Start broadcasting events to all WebSocket clients
	go s.broadcastLoop(ctx)

	// Start server in goroutine
	go func() {
		log.Printf("🌐 Face UI available at http://localhost:%d", s.port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("UI Server error: %v", err)
		}
	}()

	// Shutdown when context is cancelled
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	return nil
}

// handleWebSocket upgrades the connection and streams state events.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Allow connections from any origin
	})
	if err != nil {
		log.Printf("WebSocket accept error: %v", err)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())

	s.wsMu.Lock()
	s.wsClients[conn] = cancel
	s.wsMu.Unlock()

	log.Printf("🌐 Face UI client connected (%d total)", len(s.wsClients))

	// Send initial state snapshot
	state := s.stateTracker.GetState()
	msg := WSMessage{Type: "state", Data: state}
	_ = conn.Write(ctx, websocket.MessageText, msg.JSON())

	// Send recent history
	history := s.eventBus.GetHistory()
	histMsg := WSMessage{Type: "history", Data: history}
	_ = conn.Write(ctx, websocket.MessageText, histMsg.JSON())

	// Send initial stats
	stats := s.stateTracker.GetStats()
	statsMsg := WSMessage{Type: "stats", Data: stats}
	_ = conn.Write(ctx, websocket.MessageText, statsMsg.JSON())

	// Keep connection alive by reading (discard any client messages)
	for {
		_, _, err := conn.Read(ctx)
		if err != nil {
			break
		}
	}

	// Cleanup
	s.wsMu.Lock()
	delete(s.wsClients, conn)
	s.wsMu.Unlock()
	cancel()
	conn.Close(websocket.StatusNormalClosure, "")
	log.Printf("🌐 Face UI client disconnected (%d remaining)", len(s.wsClients))
}

// broadcastLoop listens for events and pushes them to all WebSocket clients.
func (s *Server) broadcastLoop(ctx context.Context) {
	sub := s.eventBus.Subscribe()
	defer s.eventBus.Unsubscribe(sub)

	// Periodic stats ticker
	statsTicker := time.NewTicker(5 * time.Second)
	defer statsTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case evt, ok := <-sub:
			if !ok {
				return
			}
			// Update state tracker
			s.stateTracker.HandleEvent(evt)

			// Broadcast state update to all clients
			state := s.stateTracker.GetState()
			stateMsg := WSMessage{Type: "state", Data: state}
			s.broadcast(ctx, stateMsg.JSON())

			// Broadcast a nicely formatted activity entry
			entry := s.formatActivity(evt)
			if entry.Kind != "" {
				activityMsg := WSMessage{Type: "activity", Data: entry}
				s.broadcast(ctx, activityMsg.JSON())
			}

		case <-statsTicker.C:
			stats := s.stateTracker.GetStats()
			msg := WSMessage{Type: "stats", Data: stats}
			s.broadcast(ctx, msg.JSON())
		}
	}
}

// broadcast sends a message to all connected WebSocket clients.
func (s *Server) broadcast(ctx context.Context, data []byte) {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()

	for conn := range s.wsClients {
		writeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := conn.Write(writeCtx, websocket.MessageText, data)
		cancel()
		if err != nil {
			// Client disconnected, will be cleaned up by the read loop
			continue
		}
	}
}

// handleState returns the current agent state as JSON.
func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	state := s.stateTracker.GetState()
	json.NewEncoder(w).Encode(state)
}

// handleHistory returns the recent activity timeline as JSON.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	history := s.eventBus.GetHistory()
	json.NewEncoder(w).Encode(history)
}

// handleStats returns current stats as JSON.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	stats := s.stateTracker.GetStats()
	json.NewEncoder(w).Encode(stats)
}

// formatActivity converts a raw event into a human-readable ActivityEntry.
func (s *Server) formatActivity(evt Event) ActivityEntry {
	data, _ := evt.Data.(map[string]interface{})

	entry := ActivityEntry{
		Timestamp: evt.Timestamp,
	}

	switch evt.Type {
	case EventThinkingStart:
		entry.Kind = "thinking_start"
		entry.Title = "Thinking"
		if data != nil {
			if msg, ok := data["message"].(string); ok {
				entry.Detail = truncate(msg, 120)
			}
		}

	case EventThinkingEnd:
		entry.Kind = "thinking_end"
		entry.Title = "Done Thinking"

	case EventToolCall:
		entry.Kind = "tool_call"
		toolName, _ := data["tool"].(string)
		args, _ := data["args"].(string)
		entry.Title = toolName
		if args != "" {
			entry.Detail = truncate(args, 150)
		}

	case EventToolResult:
		entry.Kind = "tool_result"
		toolName, _ := data["tool"].(string)
		result, _ := data["result"].(string)
		entry.Title = toolName + " result"
		if result != "" {
			entry.Detail = truncate(result, 150)
		}

	case EventResponseReady:
		entry.Kind = "message_out"
		entry.Title = "Response"
		if data != nil {
			if msg, ok := data["message"].(string); ok {
				entry.Detail = truncate(msg, 150)
			}
		}

	case EventMessageIn:
		entry.Kind = "message_in"
		entry.Title = "User Message"
		if data != nil {
			if msg, ok := data["message"].(string); ok {
				entry.Detail = truncate(msg, 150)
			}
		}

	case EventHeartbeat:
		entry.Kind = "heartbeat"
		entry.Title = "Heartbeat"

	case EventConsolidation:
		entry.Kind = "consolidation"
		entry.Title = "Memory Consolidation"
		entry.Detail = "Consolidation triggered"

	case EventCronFired:
		entry.Kind = "cron_fired"
		entry.Title = "Cron Job"
		if data != nil {
			if label, ok := data["label"].(string); ok {
				entry.Title = "Cron: " + label
			}
		}

	case EventCronCompleted:
		entry.Kind = "cron_completed"
		entry.Title = "Cron Done"
		if data != nil {
			if label, ok := data["label"].(string); ok {
				entry.Title = "Cron: " + label
			}
			if status, ok := data["status"].(string); ok {
				entry.Detail = status
			}
		}

	case EventMemoryWrite:
		entry.Kind = "memory_write"
		entry.Title = "Memory Updated"

	case EventEntityUpdate:
		entry.Kind = "entity_update"
		entry.Title = "Entity Updated"
		if data != nil {
			if entity, ok := data["entity"].(string); ok {
				entry.Title = "Entity: " + entity
			}
		}

	default:
		// Skip unknown events from the activity timeline
		return ActivityEntry{}
	}

	return entry
}
