package state

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// EventRecord is one stored event (the event log, spec §2.11.35). Ids are
// the event log's, not the store's.
type EventRecord struct {
	ID    int64
	At    time.Time
	Type  string
	Actor string
	Flow  string
	Data  map[string]string
}

// AppendEvents stores events in one statement (a round trip per batch);
// an id stored already is kept as it is.
func (s *sqlStore) AppendEvents(ctx context.Context, events []EventRecord) error {
	if len(events) == 0 {
		return nil
	}
	ctx = s.bind(ctx)
	var q strings.Builder
	q.WriteString(`INSERT INTO events (id, at, type, actor, flow, data) VALUES `)
	args := make([]any, 0, 6*len(events))
	for i, e := range events {
		data, err := json.Marshal(auditDetail(e.Data))
		if err != nil {
			return err
		}
		if i > 0 {
			q.WriteString(", ")
		}
		q.WriteString("(?, ?, ?, ?, ?, ?)")
		args = append(args, e.ID, e.At.UnixMilli(), textValue(e.Type), textValue(e.Actor), textValue(e.Flow), string(data))
	}
	q.WriteString(` ON CONFLICT (id) DO NOTHING`)
	_, err := s.db.ExecContext(ctx, q.String(), args...)
	return err
}

// RecentEvents returns the newest n events, oldest first, and the largest
// stored id.
func (s *sqlStore) RecentEvents(ctx context.Context, n int) ([]EventRecord, int64, error) {
	ctx = s.bind(ctx)
	var maxID int64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM events`).Scan(&maxID); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, at, type, actor, flow, data FROM events ORDER BY id DESC LIMIT ?`, n)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var out []EventRecord
	for rows.Next() {
		var e EventRecord
		var at int64
		var data string
		if err := rows.Scan(&e.ID, &at, &e.Type, &e.Actor, &e.Flow, &data); err != nil {
			return nil, 0, err
		}
		e.At = time.UnixMilli(at).UTC()
		if err := json.Unmarshal([]byte(data), &e.Data); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i] // oldest first
	}
	return out, maxID, nil
}

// DeleteEventsBefore removes the events older than t and says how many.
func (s *sqlStore) DeleteEventsBefore(ctx context.Context, t time.Time) (int, error) {
	ctx = s.bind(ctx)
	res, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE at < ?`, t.UnixMilli())
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// AppendEvents keeps events in memory (the newest maxMemEvents, by id). An
// id stored already is kept as it is; events may come out of id order.
func (s *MemStore) AppendEvents(_ context.Context, events []EventRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.eventIDs == nil {
		s.eventIDs = map[int64]bool{}
	}
	sorted := true
	for _, e := range events {
		if s.eventIDs[e.ID] {
			continue
		}
		e.At = time.UnixMilli(e.At.UnixMilli()).UTC()
		e.Type, e.Actor, e.Flow = textValue(e.Type), textValue(e.Actor), textValue(e.Flow)
		e.Data = auditDetail(e.Data)
		if n := len(s.events); n > 0 && e.ID < s.events[n-1].ID {
			sorted = false
		}
		s.events = append(s.events, e)
		s.eventIDs[e.ID] = true
	}
	if !sorted {
		sort.Slice(s.events, func(i, j int) bool { return s.events[i].ID < s.events[j].ID })
	}
	if len(s.events) > maxMemEvents {
		for _, e := range s.events[:len(s.events)-maxMemEvents] {
			delete(s.eventIDs, e.ID)
		}
		s.events = s.events[len(s.events)-maxMemEvents:]
	}
	return nil
}

// maxMemEvents bounds the memory store's events.
const maxMemEvents = 100000

// RecentEvents returns the newest n events, oldest first, and the largest
// stored id.
func (s *MemStore) RecentEvents(_ context.Context, n int) ([]EventRecord, int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var maxID int64
	if len(s.events) > 0 {
		maxID = s.events[len(s.events)-1].ID
	}
	from := max(len(s.events)-n, 0)
	return append([]EventRecord(nil), s.events[from:]...), maxID, nil
}

// DeleteEventsBefore removes the events older than t and says how many.
func (s *MemStore) DeleteEventsBefore(_ context.Context, t time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.events[:0]
	for _, e := range s.events {
		if e.At.UnixMilli() >= t.UnixMilli() {
			kept = append(kept, e)
		} else {
			delete(s.eventIDs, e.ID)
		}
	}
	n := len(s.events) - len(kept)
	s.events = kept
	return n, nil
}
