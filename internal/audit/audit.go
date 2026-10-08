// Package audit implements the AuditSink port with a local event-store adapter
// for protected-content (PHI) access logging.
package audit

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Entry is one audit/event log record.
type Entry struct {
	ID       int64
	At       time.Time
	Actor    string
	Action   string
	Resource string
	Detail   map[string]string
}

// AuditSink is the port for audit log delivery (arch §3.1).
type AuditSink interface {
	Record(ctx context.Context, e Entry) error
}

// LocalSink is the MVP AuditSink adapter: a local in-memory event store that
// also emits structured logs via slog.
type LocalSink struct {
	mu      sync.Mutex
	seq     int64
	entries []Entry // ring buffer of at most maxEntries
	next    int     // ring index of the oldest entry once full
	logger  *slog.Logger
}

// NewLocalSink returns a local audit sink.
func NewLocalSink(logger *slog.Logger) *LocalSink {
	if logger == nil {
		logger = slog.Default()
	}
	return &LocalSink{logger: logger}
}

// maxEntries bounds the in-memory history kept by LocalSink.
const maxEntries = 10000

// Record redacts sensitive detail, keeps the entry in a bounded in-memory
// history, and emits a structured log line.
func (s *LocalSink) Record(_ context.Context, e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	if e.ID == 0 { // otherwise the stored entry's id, so both match
		e.ID = s.seq
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	e.Detail = RedactSensitive(e.Detail)
	if len(s.entries) < maxEntries {
		s.entries = append(s.entries, e)
	} else {
		s.entries[s.next] = e // overwrite the oldest
		s.next = (s.next + 1) % maxEntries
	}

	s.logger.Info("audit",
		"id", e.ID,
		"actor", e.Actor,
		"action", e.Action,
		"resource", e.Resource,
		"detail", e.Detail,
	)
	return nil
}

// Entries returns a copy of the newest (up to maxEntries) entries, newest
// last.
func (s *LocalSink) Entries() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, 0, len(s.entries))
	out = append(out, s.entries[s.next:]...)
	return append(out, s.entries[:s.next]...)
}

var _ AuditSink = (*LocalSink)(nil)
