package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/observability"
	"github.com/weavster-dev/weavster/internal/pipeline"
	"github.com/weavster-dev/weavster/internal/serverconfig"
	"github.com/weavster-dev/weavster/internal/state"
)

// testPruner prunes store with cfg at a fixed "now".
func testPruner(store state.Store, cfg serverconfig.Prune, now time.Time) (*pruner, *pipeline.Pipeline, *observability.EventLog) {
	pipe := pipeline.New(store, newSink, nil, pipeline.Options{})
	events := observability.NewEventLog()
	audits, _ := store.(auditRepository)
	if audits == nil {
		audits = state.NewMemStore()
	}
	p := newPruner(cfg, messageAdapter{store: store, pipe: pipe}, audits, eventLogRecorder{events}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	p.now = func() time.Time { return now }
	p.base = context.Background() // as while the server runs
	return p, pipe, events
}

// runPass starts a pass and waits for it to end.
func runPass(t *testing.T, p *pruner) gateway.PruneRun {
	t.Helper()
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for p.Status().Running {
		if time.Now().After(deadline) {
			t.Fatal("the pass did not end")
		}
		time.Sleep(time.Millisecond)
	}
	return *p.Status().LastRun
}

func storedIDs(t *testing.T, s state.Store) string {
	t.Helper()
	ms, err := s.Search(context.Background(), state.Query{Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(ms))
	for i, m := range ms {
		ids[i] = m.ID
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

// TestPrune: a pass removes messages in a final status that are older than
// maxAgeHours, then the oldest past maxMessages; messages still in
// progress, and busy ones, are kept.
func TestPrune(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	old, recent := now.Add(-48*time.Hour), now.Add(-time.Hour)
	seed := []state.Message{
		{ID: "old-sent", Status: state.StatusSent, ReceivedAt: old},
		{ID: "old-dead", Status: state.StatusDeadLettered, ReceivedAt: old.Add(time.Minute)},
		{ID: "old-queued", Status: state.StatusQueued, ReceivedAt: old.Add(2 * time.Minute)},
		{ID: "old-busy", Status: state.StatusErrored, ReceivedAt: old.Add(3 * time.Minute)},
		{ID: "new-1", Status: state.StatusSent, ReceivedAt: recent},
		{ID: "new-2", Status: state.StatusFiltered, ReceivedAt: recent.Add(time.Minute)},
		{ID: "new-3", Status: state.StatusSent, ReceivedAt: recent.Add(2 * time.Minute)},
	}
	for _, tt := range []struct {
		name          string
		cfg           serverconfig.Prune
		removed, busy int
		left          string
	}{
		{"by age", serverconfig.Prune{MaxAgeHours: 24}, 2, 1, "new-1,new-2,new-3,old-busy,old-queued"},
		{"by count", serverconfig.Prune{MaxMessages: 4}, 3, 1, "new-2,new-3,old-busy,old-queued"},
		{"by age then count", serverconfig.Prune{MaxAgeHours: 24, MaxMessages: 3}, 4, 1, "new-3,old-busy,old-queued"},
		{"under both limits", serverconfig.Prune{MaxAgeHours: 100, MaxMessages: 10}, 0, 0, "new-1,new-2,new-3,old-busy,old-dead,old-queued,old-sent"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := state.NewMemStore()
			for _, m := range seed {
				m.FlowID = "f"
				if err := store.Put(context.Background(), m); err != nil {
					t.Fatal(err)
				}
			}
			tt.cfg.IntervalMinutes = 60
			p, pipe, events := testPruner(store, tt.cfg, now)
			release, _ := pipe.Hold("old-busy") // being processed right now
			defer release()
			run := runPass(t, p)
			if run.Removed != tt.removed || run.Busy != tt.busy || run.Stopped || run.Error != "" || run.FinishedAt == nil {
				t.Errorf("run = %+v, want removed %d busy %d", run, tt.removed, tt.busy)
			}
			if got := storedIDs(t, store); got != tt.left {
				t.Errorf("left %s, want %s", got, tt.left)
			}
			if evs := events.Search(observability.EventFilter{Type: "messages.pruned"}); len(evs) != 1 {
				t.Errorf("messages.pruned events = %+v", evs)
			}
		})
	}
}

// blockingStore's Count waits until its pass is stopped.
type blockingStore struct {
	state.Store
	entered chan struct{}
}

func (s blockingStore) Count(ctx context.Context, _ state.Query) (int, error) {
	close(s.entered)
	<-ctx.Done()
	return 0, ctx.Err()
}

// failingCountStore's Count fails.
type failingCountStore struct{ state.Store }

func (failingCountStore) Count(context.Context, state.Query) (int, error) {
	return 0, errors.New("count failed")
}

// TestPruneControl: start, stop, and status; a pass that is stopped or
// fails says so; pruning off refuses to start.
func TestPruneControl(t *testing.T) {
	now := time.Now()
	on := serverconfig.Prune{MaxMessages: 1, IntervalMinutes: 60}

	off, _, _ := testPruner(state.NewMemStore(), serverconfig.Prune{IntervalMinutes: 60}, now)
	if err := off.Start(); !errors.Is(err, gateway.ErrPruneOff) {
		t.Errorf("Start with pruning off = %v", err)
	}
	if err := off.Stop(); !errors.Is(err, gateway.ErrPruneNotRunning) {
		t.Errorf("Stop with nothing running = %v", err)
	}
	if st := off.Status(); st.Running || st.LastRun != nil || st.NextRun != nil {
		t.Errorf("status before any pass = %+v", st)
	}

	block := blockingStore{Store: state.NewMemStore(), entered: make(chan struct{})}
	p, _, events := testPruner(block, on, now)
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	<-block.entered
	if err := p.Start(); !errors.Is(err, gateway.ErrPruneRunning) {
		t.Errorf("second Start = %v", err)
	}
	if st := p.Status(); !st.Running || st.LastRun == nil || st.LastRun.FinishedAt != nil {
		t.Errorf("status while running = %+v", st)
	}
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
	if st := p.Status(); st.Running || !st.LastRun.Stopped || st.LastRun.FinishedAt == nil {
		t.Errorf("status after Stop = %+v", st)
	}
	if evs := events.Search(observability.EventFilter{Type: "messages.pruned"}); len(evs) != 1 || evs[0].Data["stopped"] != "true" {
		t.Errorf("stopped pass event = %+v", evs)
	}

	// Outside the server's lifetime no pass starts.
	idle, _, _ := testPruner(state.NewMemStore(), on, now)
	idle.base = nil
	if err := idle.Start(); !errors.Is(err, gateway.ErrPruneUnavailable) {
		t.Errorf("Start before the loop = %v", err)
	}
	ended, cancelEnded := context.WithCancel(context.Background())
	cancelEnded()
	idle.base = ended // shutting down, before the loop has noticed
	if err := idle.Start(); !errors.Is(err, gateway.ErrPruneUnavailable) {
		t.Errorf("Start while shutting down = %v", err)
	}

	failing, _, events := testPruner(failingCountStore{state.NewMemStore()}, on, now)
	if run := runPass(t, failing); run.Error != "count failed" {
		t.Errorf("failed pass = %+v", run)
	}
	if evs := events.Search(observability.EventFilter{Type: "messages.pruned"}); len(evs) != 1 || evs[0].Data["error"] != "count failed" {
		t.Errorf("failed pass event = %+v", evs)
	}
}

// TestPruneLoop: the loop runs a pass every interval, shows the next one,
// and stops with the server.
func TestPruneLoop(t *testing.T) {
	store := state.NewMemStore()
	for _, id := range []string{"a", "b", "c"} {
		_ = store.Put(context.Background(), state.Message{ID: id, FlowID: "f", Status: state.StatusSent})
	}
	p, _, _ := testPruner(store, serverconfig.Prune{MaxMessages: 1, IntervalMinutes: 1}, time.Now())
	p.base, p.interval = nil, 10*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.loop(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for storedIDs(t, store) != "c" {
		if time.Now().After(deadline) {
			t.Fatalf("left %s, want c", storedIDs(t, store))
		}
		time.Sleep(5 * time.Millisecond)
	}
	if st := p.Status(); st.NextRun == nil {
		t.Errorf("status = %+v, want the next run", st)
	}
	cancel()
	<-done
	if err := p.Start(); !errors.Is(err, gateway.ErrPruneUnavailable) {
		t.Errorf("Start after the loop ended = %v", err)
	}
	if st := p.Status(); st.NextRun != nil {
		t.Errorf("status after the loop = %+v, want no next run", st)
	}
}

// TestPruneAudit: prune.auditMaxAgeDays removes older audit entries only;
// the pass reports how many.
func TestPruneAudit(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	store := state.NewMemStore()
	ctx := context.Background()
	for _, at := range []time.Time{now.AddDate(0, 0, -40), now.AddDate(0, 0, -31), now.AddDate(0, 0, -1)} {
		if _, err := store.AppendAudit(ctx, state.AuditRecord{At: at, Actor: "a", Action: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	p, _, events := testPruner(store, serverconfig.Prune{AuditMaxAgeDays: 30, IntervalMinutes: 60}, now)
	if run := runPass(t, p); run.AuditRemoved != 2 || run.Removed != 0 {
		t.Errorf("run = %+v, want 2 audit entries removed", run)
	}
	if left, _ := store.SearchAudit(ctx, state.AuditQuery{}); len(left) != 1 {
		t.Errorf("left %d entries, want 1", len(left))
	}
	if evs := events.Search(observability.EventFilter{Type: "messages.pruned"}); len(evs) != 1 || evs[0].Data["auditRemoved"] != "2" {
		t.Errorf("event = %+v", evs)
	}
}
