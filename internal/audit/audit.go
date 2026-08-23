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
	entries []Entry
	logger  *slog.Logger
}

// NewLocalSink returns a local audit sink.
func NewLocalSink(logger *slog.Logger) *LocalSink {
	if logger == nil {
		logger = slog.Default()
	}
	return &LocalSink{logger: logger}
}

// Record appends an entry and emits a structured log line.
func (s *LocalSink) Record(_ context.Context, e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	e.ID = s.seq
	e.At = time.Now()
	s.entries = append(s.entries, e)

	s.logger.Info("audit",
		"id", e.ID,
		"actor", e.Actor,
		"action", e.Action,
		"resource", e.Resource,
	)
	return nil
}

// Entries returns a copy of all recorded entries (newest last).
func (s *LocalSink) Entries() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, len(s.entries))
	copy(out, s.entries)
	return out
}

var _ AuditSink = (*LocalSink)(nil)
