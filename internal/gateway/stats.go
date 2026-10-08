package gateway

import (
	"context"
	"errors"
	"fmt"
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
	LastMessageAt *time.Time                `json:"lastMessageAt,omitempty"` // absent until the first message
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

// StatsSample is one flow's lifetime statistics at one sampling time
// (spec §2.11.37).
type StatsSample struct {
	At     time.Time `json:"at"`
	FlowID string    `json:"flowId"`
	Stats  FlowStats `json:"stats"`
}

// StatsSeriesQuery narrows a statistics time-series read.
type StatsSeriesQuery struct {
	FlowID   string    // empty = every flow
	From, To time.Time // at or after / at or before; zero = open
	Limit    int       // the newest N matching samples
}

// Statistics time-series limits.
const (
	DefaultStatsSeriesLimit = 1000
	MaxStatsSeriesLimit     = 10000
)

// StatsHistory reads the statistics time series.
type StatsHistory interface {
	// StatsSeries returns the newest q.Limit matching samples, oldest first;
	// ErrFlowNotFound when q.FlowID names no flow.
	StatsSeries(ctx context.Context, q StatsSeriesQuery) ([]StatsSample, error)
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
	Type     string
	FlowID   string
	From, To time.Time // at or after / at or before; zero = open
	AfterID  int64     // only events with a larger id
	Cursor   bool      // afterId was given (even 0): the limit keeps the oldest
	Limit    int       // newest N matches; 0 = all
}

// Event search limits.
const (
	DefaultEventLimit = 1000
	MaxEventLimit     = 10000
)

// EventSearcher reads the event log.
type EventSearcher interface {
	SearchEvents(ctx context.Context, q EventQuery) ([]Event, error)
	// GetEvent returns one event (ErrEventNotFound when unknown or no
	// longer kept).
	GetEvent(ctx context.Context, id int64) (Event, error)
	// CountEvents counts the matches (q.Limit is ignored).
	CountEvents(ctx context.Context, q EventQuery) (int, error)
	// MaxEventID is the id of the newest event, 0 when there is none.
	MaxEventID(ctx context.Context) (int64, error)
}

// ErrEventNotFound: no event has that id (or it is no longer kept).
var ErrEventNotFound = errors.New("event not found")

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

func (s *Server) handleStatsSeries(w http.ResponseWriter, r *http.Request) {
	if s.cfg.StatsHistory == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "statistics unavailable")
		return
	}
	v := r.URL.Query()
	q := StatsSeriesQuery{FlowID: v.Get("flowId"), Limit: DefaultStatsSeriesLimit}
	if msg := timeRange(v, &q.From, &q.To); msg != "" {
		writeStatusError(w, http.StatusBadRequest, msg)
		return
	}
	if raw := v.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > MaxStatsSeriesLimit {
			writeStatusError(w, http.StatusBadRequest, "limit must be between 1 and 10000")
			return
		}
		q.Limit = n
	}
	samples, err := s.cfg.StatsHistory.StatsSeries(r.Context(), q)
	if err != nil {
		writeFlowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, samples)
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

func (s *Server) eventsAvailable(w http.ResponseWriter) bool {
	if s.cfg.Events == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "events unavailable")
		return false
	}
	return true
}

// eventQuery reads the event filters (type, flowId, from, to, afterId) and,
// with withLimit, limit; on a bad value it answers 400 and returns false.
func eventQuery(w http.ResponseWriter, r *http.Request, withLimit bool) (EventQuery, bool) {
	v := r.URL.Query()
	q := EventQuery{Type: v.Get("type"), FlowID: v.Get("flowId")}
	bad := func(msg string) (EventQuery, bool) {
		writeStatusError(w, http.StatusBadRequest, msg)
		return q, false
	}
	if msg := timeRange(v, &q.From, &q.To); msg != "" {
		return bad(msg)
	}
	if raw := v.Get("afterId"); raw != "" {
		n, msg := parseAfterID(raw)
		if msg != "" {
			return bad(msg)
		}
		q.AfterID, q.Cursor = n, true
	}
	if withLimit {
		n, msg := parseLimit(v.Get("limit"), DefaultEventLimit, MaxEventLimit)
		if msg != "" {
			return bad(msg)
		}
		q.Limit = n
	}
	return q, true
}

// parseAfterID reads an afterId cursor; it returns what is wrong, or "".
func parseAfterID(raw string) (int64, string) {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0, "afterId must be a whole number from 0 to 9223372036854775807"
	}
	return n, ""
}

// parseLimit reads a page size (def when raw is ""); it returns what is
// wrong, or "".
func parseLimit(raw string, def, maxLimit int) (int, string) {
	if raw == "" {
		return def, ""
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > maxLimit {
		return 0, fmt.Sprintf("limit must be between 1 and %d", maxLimit)
	}
	return n, ""
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if !s.eventsAvailable(w) {
		return
	}
	q, ok := eventQuery(w, r, true)
	if !ok {
		return
	}
	events, err := s.cfg.Events.SearchEvents(r.Context(), q)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) handleEventGet(w http.ResponseWriter, r *http.Request) {
	if !s.eventsAvailable(w) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeStatusError(w, http.StatusBadRequest, "the event id must be a positive number")
		return
	}
	e, err := s.cfg.Events.GetEvent(r.Context(), id)
	if errors.Is(err, ErrEventNotFound) {
		writeStatusError(w, http.StatusNotFound, "event not found (it may be older than the events kept)")
		return
	}
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) handleEventCount(w http.ResponseWriter, r *http.Request) {
	if !s.eventsAvailable(w) {
		return
	}
	q, ok := eventQuery(w, r, false)
	if !ok {
		return
	}
	n, err := s.cfg.Events.CountEvents(r.Context(), q)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": n})
}

func (s *Server) handleEventMaxID(w http.ResponseWriter, r *http.Request) {
	if !s.eventsAvailable(w) {
		return
	}
	id, err := s.cfg.Events.MaxEventID(r.Context())
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"maxId": id})
}

// handleEventExport sends every match (no limit) as a JSON file.
func (s *Server) handleEventExport(w http.ResponseWriter, r *http.Request) {
	if !s.eventsAvailable(w) {
		return
	}
	q, ok := eventQuery(w, r, false)
	if !ok {
		return
	}
	events, err := s.cfg.Events.SearchEvents(r.Context(), q)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	name := "events.json"
	if wantsXML(w) {
		name = "events.xml"
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	writeJSON(w, http.StatusOK, events)
}
