package observability

import (
	"sort"
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
	Type    string
	Flow    string
	Since   time.Time // at or after; zero = open
	Until   time.Time // at or before; zero = open
	AfterID int64     // only ids above this (polling for new events)
	// Cursor marks a poll from AfterID (even 0): Limit then keeps the
	// oldest N after it, so polling never skips events.
	Cursor bool
	// Limit keeps N matches, 0 = all: the newest N, or the oldest N with
	// Cursor.
	Limit int
}

func (f EventFilter) matches(e Event) bool {
	switch {
	case f.Type != "" && e.Type != f.Type,
		f.Flow != "" && e.Flow != f.Flow,
		!f.Since.IsZero() && e.At.Before(f.Since),
		!f.Until.IsZero() && e.At.After(f.Until),
		e.ID <= f.AfterID:
		return false
	}
	return true
}

// EventLog is an in-memory event store with search, count, and export
// (spec §2.11.35).
type EventLog struct {
	mu     sync.Mutex
	seq    int64
	events []Event // ring buffer of at most MaxEvents
	next   int     // index of the oldest event once full
	newest int64   // the newest event's id (0 before any)
	sink   func(Event)
	// sinkMu is held (read) while an event goes to the sink, so SetSink
	// returns only once no event is still on its way to the old one.
	sinkMu sync.RWMutex
}

// SetSink sends every event added from now on to sink too (after it is
// logged, outside the log's lock): the store's writer. It returns once
// events already on their way to the previous sink have reached it.
func (l *EventLog) SetSink(sink func(Event)) {
	l.sinkMu.Lock()
	defer l.sinkMu.Unlock()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sink = sink
}

// Load replaces the log with events (oldest first; the newest MaxEvents are
// kept), and continues ids after maxID: events kept from before a restart.
func (l *EventLog) Load(events []Event, maxID int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(events) > MaxEvents {
		events = events[len(events)-MaxEvents:]
	}
	l.events, l.next = append([]Event(nil), events...), 0
	l.newest = 0
	if len(l.events) > 0 {
		l.newest = l.events[len(l.events)-1].ID
	}
	if maxID > l.seq {
		l.seq = maxID // new ids start after it; MaxID stays the newest event's
	}
}

// NewEventLog returns an empty event log.
func NewEventLog() *EventLog { return &EventLog{} }

// MaxEvents bounds the in-memory event history; older events are dropped.
const MaxEvents = 10000

// Add records an event and returns it.
func (l *EventLog) Add(typ, actor, flow string, data map[string]string) Event {
	l.sinkMu.RLock()
	defer l.sinkMu.RUnlock()
	l.mu.Lock()
	l.seq++
	l.newest = l.seq
	e := Event{ID: l.seq, At: time.Now(), Type: typ, Actor: actor, Flow: flow, Data: data}
	if len(l.events) < MaxEvents {
		l.events = append(l.events, e)
	} else {
		l.events[l.next] = e // overwrite the oldest
		l.next = (l.next + 1) % MaxEvents
	}
	sink := l.sink
	l.mu.Unlock()
	if sink != nil {
		sink(e)
	}
	return e
}

// Search returns events matching the filter.
func (l *EventLog) Search(f EventFilter) []Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Event, 0)
	for i := range l.events {
		if e := l.events[(l.next+i)%len(l.events)]; f.matches(e) {
			out = append(out, e)
		}
	}
	if f.Limit > 0 && len(out) > f.Limit {
		if f.Cursor {
			return out[:f.Limit] // the oldest Limit after the cursor
		}
		out = out[len(out)-f.Limit:] // the newest Limit, oldest first
	}
	return out
}

// Get returns the event with id, if it is still kept. Ids ascend around the
// ring (with gaps after a restart), so it is found by binary search.
func (l *EventLog) Get(id int64) (Event, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := len(l.events)
	at := func(i int) Event { return l.events[(l.next+i)%n] }
	i := sort.Search(n, func(i int) bool { return at(i).ID >= id })
	if i == n || at(i).ID != id {
		return Event{}, false
	}
	return at(i), true
}

// MaxID returns the id of the newest event (0 when none was recorded).
func (l *EventLog) MaxID() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.newest
}

// Count returns the number of events matching the filter (Limit is
// ignored).
func (l *EventLog) Count(f EventFilter) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, e := range l.events {
		if f.matches(e) {
			n++
		}
	}
	return n
}

// Export returns events matching the filter (same as Search; the export path
// serializes to a file at the API layer).
func (l *EventLog) Export(f EventFilter) []Event { return l.Search(f) }
