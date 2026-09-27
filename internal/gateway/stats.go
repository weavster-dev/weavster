package gateway

import (
	"context"
	"net/http"
	"strconv"
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
	Limit  int // newest N matches
}

// Event search limits.
const (
	DefaultEventLimit = 1000
	MaxEventLimit     = 10000
)

// EventSearcher searches the event log.
type EventSearcher interface {
	SearchEvents(ctx context.Context, q EventQuery) ([]Event, error)
}

func (s *Server) handleFlowStats(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Stats == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "statistics unavailable")
		return
	}
	lifetime := false
	if v := r.URL.Query().Get("lifetime"); v != "" {
		var err error
		if lifetime, err = strconv.ParseBool(v); err != nil {
			writeStatusError(w, http.StatusBadRequest, "lifetime must be true or false")
			return
		}
	}
	st, err := s.cfg.Stats.FlowStats(r.Context(), r.PathValue("id"), lifetime)
	if err != nil {
		writeFlowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Events == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "events unavailable")
		return
	}
	limit := DefaultEventLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > MaxEventLimit {
			writeStatusError(w, http.StatusBadRequest, "limit must be between 1 and 10000")
			return
		}
		limit = n
	}
	events, err := s.cfg.Events.SearchEvents(r.Context(), EventQuery{Type: r.URL.Query().Get("type"), FlowID: r.URL.Query().Get("flowId"), Limit: limit})
	if err != nil {
		writeStatusError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, events)
}
