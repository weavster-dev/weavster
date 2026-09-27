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
	// AllFlowStats returns every flow's statistics by flow id.
	AllFlowStats(ctx context.Context, lifetime bool) (map[string]FlowStats, error)
	// ResetStats clears a flow's (all flows' when flowID is empty) current
	// statistics, and with lifetime its lifetime totals too.
	ResetStats(ctx context.Context, flowID string, lifetime bool) error
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
	lifetime, ok := lifetimeParam(w, r)
	if !ok {
		return
	}
	st, err := s.cfg.Stats.FlowStats(r.Context(), r.PathValue("id"), lifetime)
	if err != nil {
		writeFlowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// lifetimeParam reads the optional lifetime=true|false query parameter; on
// a bad value it answers 400 and returns false.
func lifetimeParam(w http.ResponseWriter, r *http.Request) (lifetime, ok bool) {
	v := r.URL.Query().Get("lifetime")
	if v == "" {
		return false, true
	}
	lifetime, err := strconv.ParseBool(v)
	if err != nil {
		writeStatusError(w, http.StatusBadRequest, "lifetime must be true or false")
		return false, false
	}
	return lifetime, true
}

func (s *Server) handleAllFlowStats(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Stats == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "statistics unavailable")
		return
	}
	lifetime, ok := lifetimeParam(w, r)
	if !ok {
		return
	}
	all, err := s.cfg.Stats.AllFlowStats(r.Context(), lifetime)
	if err != nil {
		writeFlowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, all)
}

// handleResetStats clears one flow's statistics ({id} in the path) or every
// flow's.
func (s *Server) handleResetStats(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Stats == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "statistics unavailable")
		return
	}
	lifetime, ok := lifetimeParam(w, r)
	if !ok {
		return
	}
	if err := s.cfg.Stats.ResetStats(r.Context(), r.PathValue("id"), lifetime); err != nil {
		writeFlowError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}
