package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// busyLogInterval is how often a refusal for lack of a slot is logged at
// most, so saturation shows in the log without flooding it.
const busyLogInterval = 10 * time.Second

// processLimit bounds the messages received and processed at once (#107
// D-79): a slot per message, and a bounded wait for one when all are busy.
type processLimit struct {
	slots   chan struct{}
	wait    time.Duration
	stopped <-chan struct{} // the server is stopping: nobody waits any longer
	logger  *slog.Logger

	mu       sync.Mutex
	loggedAt time.Time
	refused  int   // refusals since the last log line
	total    int64 // every refusal (weavster_processing_refused_total)
}

func newProcessLimit(cfg serverconfig.Processing, stopped <-chan struct{}, logger *slog.Logger) *processLimit {
	return &processLimit{slots: make(chan struct{}, cfg.MaxConcurrent), wait: time.Duration(cfg.WaitMs) * time.Millisecond,
		stopped: stopped, logger: logger}
}

// acquire takes a slot, waiting up to the limit's wait; it returns
// gateway.ErrBusy when none frees up in time or the server is stopping,
// or ctx's error. The returned func gives the slot back.
func (l *processLimit) acquire(ctx context.Context) (func(), error) {
	release := func() { <-l.slots }
	select {
	case l.slots <- struct{}{}:
		return release, nil
	default:
	}
	timer := time.NewTimer(l.wait)
	defer timer.Stop()
	select {
	case l.slots <- struct{}{}:
		return release, nil
	case <-timer.C:
	case <-l.stopped:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	l.noteRefusal()
	return nil, gateway.ErrBusy
}

// noteRefusal logs refusals for lack of a slot, at most once per
// busyLogInterval, with how many there were.
func (l *processLimit) noteRefusal() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refused++
	l.total++
	if l.logger == nil || time.Since(l.loggedAt) < busyLogInterval {
		return
	}
	l.logger.Warn("processing limit reached: messages refused as busy (see processing.maxConcurrent)",
		"refused", l.refused, "maxConcurrent", cap(l.slots))
	l.loggedAt, l.refused = time.Now(), 0
}

// usage is how many slots are taken, how many there are, and how many
// messages were refused for lack of one.
func (l *processLimit) usage() (inFlight, slots int, refused int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.slots), cap(l.slots), l.total
}
