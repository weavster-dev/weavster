package observability

import (
	"io"
	"log/slog"
	"sync"
)

// NewLogger returns a structured slog JSON logger writing to w at level.
func NewLogger(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
}

// LogRing is a bounded in-memory buffer of log lines backing the server log
// viewer (spec §2.11.37).
type LogRing struct {
	mu    sync.Mutex
	lines []string
	limit int
}

// NewLogRing returns a log ring holding at most limit lines.
func NewLogRing(limit int) *LogRing {
	return &LogRing{limit: limit}
}

// Add appends a line, evicting the oldest once the ring is full.
func (r *LogRing) Add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
	if len(r.lines) > r.limit {
		r.lines = r.lines[len(r.lines)-r.limit:]
	}
}

// Tail returns up to n most-recent lines (newest last).
func (r *LogRing) Tail(n int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n <= 0 || n > len(r.lines) {
		n = len(r.lines)
	}
	out := make([]string, n)
	copy(out, r.lines[len(r.lines)-n:])
	return out
}
