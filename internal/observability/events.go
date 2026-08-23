package observability

import (
	"sync"
	"time"
)

// Event is a single entry in the operational/administrative event log
// (spec §2.11.35).
type Event struct {
	ID    int64
	At    time.Time
	Type  string
	Actor string
	Flow  string
	Data  map[string]string
}

// EventFilter narrows Search/Count/Export.
type EventFilter struct {
	Type  string
	Flow  string
	Since time.Time
}

func (f EventFilter) matches(e Event) bool {
	if f.Type != "" && e.Type != f.Type {
		return false
	}
	if f.Flow != "" && e.Flow != f.Flow {
		return false
	}
	if !f.Since.IsZero() && e.At.Before(f.Since) {
		return false
	}
	return true
}

// EventLog is an in-memory event store with search, count, and export
// (spec §2.11.35).
type EventLog struct {
	mu     sync.Mutex
	seq    int64
	events []Event
}

// NewEventLog returns an empty event log.
func NewEventLog() *EventLog { return &EventLog{} }

// Add records an event and returns it.
func (l *EventLog) Add(typ, actor, flow string, data map[string]string) Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	e := Event{ID: l.seq, At: time.Now(), Type: typ, Actor: actor, Flow: flow, Data: data}
	l.events = append(l.events, e)
	return e
}

// Search returns events matching the filter.
func (l *EventLog) Search(f EventFilter) []Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Event, 0)
	for _, e := range l.events {
		if f.matches(e) {
			out = append(out, e)
		}
	}
	return out
}

// Count returns the number of events matching the filter.
func (l *EventLog) Count(f EventFilter) int { return len(l.Search(f)) }

// Export returns events matching the filter (same as Search; the export path
// serializes to a file at the API layer).
func (l *EventLog) Export(f EventFilter) []Event { return l.Search(f) }
