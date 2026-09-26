package gateway

import (
	"context"
	"net/http"
	"time"
)

// ConnectorStats counts traffic for one destination of a flow.
type ConnectorStats struct {
	Sent    int64 `json:"sent"`
	Errored int64 `json:"errored"`
}

// FlowStats are a flow's message counters (spec §2.11.36).
type FlowStats struct {
	Received      int64                     `json:"received"`
	Filtered      int64                     `json:"filtered"`
	Transformed   int64                     `json:"transformed"`
	Sent          int64                     `json:"sent"`
	Errored       int64                     `json:"errored"`
	Queued        int64                     `json:"queued"`
	Destinations  map[string]ConnectorStats `json:"destinations"`
	LastMessageAt *time.Time                `json:"lastMessageAt"`
}

// StatsProvider reports flow statistics.
type StatsProvider interface {
	FlowStats(ctx context.Context, flowID string, lifetime bool) (FlowStats, error)
}

// Event is one entry of the event log.
type Event struct {
	ID     int64             `json:"id"`
	At     time.Time         `json:"at"`
	Type   string            `json:"type"`
	FlowID string            `json:"flowId,omitempty"`
	Data   map[string]string `json:"data,omitempty"`
}

// EventQuery narrows an event search.
type EventQuery struct {
	Type   string
	FlowID string
}

// EventSearcher searches the event log.
type EventSearcher interface {
	SearchEvents(ctx context.Context, q EventQuery) ([]Event, error)
}

func (s *Server) handleFlowStats(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Stats == nil {
		http.Error(w, "statistics unavailable", http.StatusServiceUnavailable)
		return
	}
	st, err := s.cfg.Stats.FlowStats(r.Context(), r.PathValue("id"), r.URL.Query().Get("lifetime") == "true")
	if err != nil {
		writeFlowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Events == nil {
		http.Error(w, "events unavailable", http.StatusServiceUnavailable)
		return
	}
	events, err := s.cfg.Events.SearchEvents(r.Context(), EventQuery{Type: r.URL.Query().Get("type"), FlowID: r.URL.Query().Get("flowId")})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, events)
}
