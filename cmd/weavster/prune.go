package main

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/serverconfig"
	"github.com/weavster-dev/weavster/internal/state"
)

// finalStatuses are the statuses of messages a prune may remove: nothing
// more happens to them. Received, transformed, and queued messages are
// never pruned.
var finalStatuses = []state.Status{state.StatusSent, state.StatusFiltered, state.StatusErrored, state.StatusDeadLettered}

// pruneRound bounds how many messages one count round looks at to find
// its cutoff.
const pruneRound = 1000

// pruner removes old messages every interval and on demand (spec §2.6.23,
// #107 D-88): those received more than MaxAgeHours ago, then the oldest
// past MaxMessages. It removes through the message adapter, which holds
// each message and checks it again first, so a message being processed is
// skipped (busy) and never removed. Passes run within the server's
// lifetime: once loop has ended, none starts.
type pruner struct {
	cfg      serverconfig.Prune
	msgs     messageAdapter
	audits   auditRepository
	events   eventLogRecorder
	logger   *slog.Logger
	now      func() time.Time
	interval time.Duration // cfg.IntervalMinutes

	mu      sync.Mutex
	base    context.Context    // the server's, while loop runs
	cancel  context.CancelFunc // the running pass
	done    chan struct{}      // closed when the running pass ends
	last    *gateway.PruneRun
	nextRun time.Time
}

func newPruner(cfg serverconfig.Prune, msgs messageAdapter, audits auditRepository, events eventLogRecorder, logger *slog.Logger) *pruner {
	return &pruner{cfg: cfg, msgs: msgs, audits: audits, events: events, logger: logger, now: time.Now,
		interval: time.Duration(cfg.IntervalMinutes) * time.Minute}
}

// loop runs a pass every interval until ctx ends, then stops the running
// pass, waits for it, and refuses new ones.
func (p *pruner) loop(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	p.mu.Lock()
	p.base, p.nextRun = ctx, p.now().Add(p.interval)
	p.mu.Unlock()
	for {
		select {
		case <-ctx.Done():
			p.mu.Lock()
			p.base = nil
			p.mu.Unlock()
			_ = p.Stop()
			return
		case <-ticker.C:
			p.mu.Lock()
			p.nextRun = p.now().Add(p.interval)
			p.mu.Unlock()
			if err := p.Start(); errors.Is(err, gateway.ErrPruneRunning) {
				p.logger.Warn("prune pass skipped: the previous one is still running")
			}
		}
	}
}

// Start runs a pass in the background.
func (p *pruner) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch {
	case !p.cfg.Enabled():
		return gateway.ErrPruneOff
	case p.base == nil || p.base.Err() != nil: // starting, or shutting down
		return gateway.ErrPruneUnavailable
	case p.cancel != nil:
		return gateway.ErrPruneRunning
	}
	ctx, cancel := context.WithCancel(p.base)
	run := &gateway.PruneRun{StartedAt: p.now().UTC()}
	p.cancel, p.done, p.last = cancel, make(chan struct{}), run
	go p.pass(ctx, run, p.done)
	return nil
}

// Stop ends the running pass and waits until it has.
func (p *pruner) Stop() error {
	p.mu.Lock()
	cancel, done := p.cancel, p.done
	p.mu.Unlock()
	if cancel == nil {
		return gateway.ErrPruneNotRunning
	}
	cancel()
	<-done
	return nil
}

func (p *pruner) Status() gateway.PruneStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := gateway.PruneStatus{
		MaxAgeHours: p.cfg.MaxAgeHours, MaxMessages: p.cfg.MaxMessages, AuditMaxAgeDays: p.cfg.AuditMaxAgeDays, IntervalMinutes: p.cfg.IntervalMinutes,
		Running: p.cancel != nil,
	}
	if p.cfg.Enabled() && p.base != nil {
		next := p.nextRun.UTC()
		st.NextRun = &next
	}
	if p.last != nil {
		last := *p.last
		st.LastRun = &last
	}
	return st
}

func (p *pruner) pass(ctx context.Context, run *gateway.PruneRun, done chan struct{}) {
	removed, busy, err := p.prune(ctx)
	audits := 0
	if err == nil && p.cfg.AuditMaxAgeDays > 0 {
		audits, err = p.audits.DeleteAuditBefore(ctx, p.now().AddDate(0, 0, -p.cfg.AuditMaxAgeDays))
	}
	p.mu.Lock()
	finished := p.now().UTC()
	run.FinishedAt, run.Removed, run.Busy, run.AuditRemoved = &finished, removed, busy, audits
	switch {
	case errors.Is(err, context.Canceled):
		run.Stopped = true
	case err != nil:
		run.Error = err.Error()
	}
	outcome := *run
	p.cancel, p.done = nil, nil
	p.mu.Unlock()
	// done closes last: once Stop returns, the pass's event and log line
	// are there.
	defer close(done)
	data := map[string]string{"removed": strconv.Itoa(outcome.Removed), "busy": strconv.Itoa(outcome.Busy), "auditRemoved": strconv.Itoa(outcome.AuditRemoved)}
	if outcome.Stopped {
		data["stopped"] = "true"
	}
	if outcome.Error != "" {
		data["error"] = outcome.Error
		p.logger.Warn("prune pass failed", "removed", outcome.Removed, "error", outcome.Error)
	}
	p.events.record("messages.pruned", "", data)
}

// prune removes by age, then by count. busy counts each skipped message
// once, whichever phases skipped it.
func (p *pruner) prune(ctx context.Context) (removed, busy int, err error) {
	skipped := map[string]bool{}
	remove := func(cutoff time.Time) (int, error) {
		n, held, err := p.msgs.removeMatching(ctx, state.Query{To: cutoff}, func(m state.Message) bool { return isFinal(m.Status) })
		for _, id := range held {
			skipped[id] = true
		}
		return n, err
	}
	defer func() { busy = len(skipped) }()
	if p.cfg.MaxAgeHours > 0 {
		n, err := remove(p.now().Add(-time.Duration(p.cfg.MaxAgeHours) * time.Hour))
		if removed += n; err != nil {
			return removed, 0, err
		}
	}
	for p.cfg.MaxMessages > 0 {
		total, err := p.msgs.store.Count(ctx, state.Query{})
		if err != nil {
			return removed, 0, err
		}
		excess := total - p.cfg.MaxMessages
		if excess <= 0 {
			break
		}
		cutoff, ok, err := p.countCutoff(ctx, min(excess+len(skipped), pruneRound))
		if err != nil || !ok {
			return removed, 0, err // nothing finished left to remove
		}
		n, err := remove(cutoff)
		if removed += n; err != nil || n == 0 {
			return removed, 0, err // only busy ones left before the cutoff
		}
	}
	return removed, 0, nil
}

// countCutoff is the receive time of the n-th oldest finished message:
// removing the finished messages received up to it removes at least n (a
// few more when several share that instant). ok is false when there is
// none.
func (p *pruner) countCutoff(ctx context.Context, n int) (cutoff time.Time, ok bool, err error) {
	var times []time.Time
	for _, st := range finalStatuses {
		page, err := p.msgs.store.ReceivedTimes(ctx, state.Query{Status: st, Sort: "received_at", Limit: n})
		if err != nil {
			return time.Time{}, false, err
		}
		times = append(times, page...)
	}
	if len(times) == 0 {
		return time.Time{}, false, nil
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	return times[min(n, len(times))-1], true, nil
}

func isFinal(s state.Status) bool {
	for _, f := range finalStatuses {
		if s == f {
			return true
		}
	}
	return false
}
