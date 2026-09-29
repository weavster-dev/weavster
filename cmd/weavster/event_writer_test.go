package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/observability"
	"github.com/weavster-dev/weavster/internal/state"
)

// countingEvents records every batch it is given, or fails.
type countingEvents struct {
	*state.MemStore
	mu      sync.Mutex
	batches []int
	fail    bool
}

func (c *countingEvents) AppendEvents(ctx context.Context, events []state.EventRecord) error {
	c.mu.Lock()
	c.batches = append(c.batches, len(events))
	fail := c.fail
	c.mu.Unlock()
	if fail {
		return errors.New("store down")
	}
	return c.MemStore.AppendEvents(ctx, events)
}

func (c *countingEvents) RecentEvents(ctx context.Context, n int) ([]state.EventRecord, int64, error) {
	if c.fail {
		return nil, 0, errors.New("store down")
	}
	return c.MemStore.RecentEvents(ctx, n)
}

// TestEventWriter: stored events come back after a "restart" with ids
// continuing after them; events are written in batches, flushed at stop,
// dropped from the store (and reported) when the queue is full, and a
// failing store is logged.
func TestEventWriter(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	repo := &countingEvents{MemStore: state.NewMemStore()}

	log := observability.NewEventLog()
	w := newEventWriter(repo, logger)
	w.every = time.Hour // only size and stop flush
	if err := w.restore(context.Background(), log, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.loop(ctx); close(done) }()
	for range eventBatch + 5 {
		log.Add("message.sent", "", "adt", map[string]string{"messageId": "m"})
	}
	cancel()
	<-done
	stored, maxID, _ := repo.RecentEvents(context.Background(), 1000)
	if len(stored) != eventBatch+5 || maxID != eventBatch+5 {
		t.Fatalf("stored %d up to %d, want %d", len(stored), maxID, eventBatch+5)
	}
	if len(repo.batches) != 2 || repo.batches[0] != eventBatch || repo.batches[1] != 5 {
		t.Errorf("batches %v, want [%d 5]", repo.batches, eventBatch)
	}

	// After a restart: the events are back and new ids follow them.
	restarted := observability.NewEventLog()
	w2 := newEventWriter(repo, logger)
	if err := w2.restore(context.Background(), restarted, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	if n := restarted.Count(observability.EventFilter{}); n != eventBatch+5 {
		t.Errorf("restored %d events", n)
	}
	if e := restarted.Add("x", "", "", nil); e.ID != eventBatch+6 {
		t.Errorf("next id %d, want %d", e.ID, eventBatch+6)
	}

	// A full queue drops events from the store and says so; a failing
	// store is logged.
	full := newEventWriter(repo, logger)
	for range eventQueue + 3 {
		full.add(observability.Event{ID: 1})
	}
	repo.fail = true
	full.write(context.Background(), []state.EventRecord{{ID: 99999}})
	if !strings.Contains(logs.String(), "dropped=3") || !strings.Contains(logs.String(), "events not stored") {
		t.Errorf("log = %s", logs.String())
	}
	if err := newEventWriter(repo, logger).restore(context.Background(), observability.NewEventLog(), time.Now()); err == nil {
		t.Error("restore from a failing store = nil error")
	}
}

// TestEventWriterTicks: an event waits at most one flush interval.
func TestEventWriterTicks(t *testing.T) {
	repo := &countingEvents{MemStore: state.NewMemStore()}
	w := newEventWriter(repo, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	w.every = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.loop(ctx)
	w.add(observability.Event{ID: 1, At: time.Now(), Type: "x"})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if got, _, _ := repo.RecentEvents(context.Background(), 10); len(got) == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the event was not written on a tick")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestEventWriterLifecycle: after a restart new ids start no lower than
// the time in microseconds (an unstored id is never reused); stop stores
// what is queued and sends no more events to the writer.
func TestEventWriterLifecycle(t *testing.T) {
	repo := &countingEvents{MemStore: state.NewMemStore()}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	log := observability.NewEventLog()
	w := newEventWriter(repo, logger)
	w.every = time.Hour
	now := time.Now()
	if err := w.restore(context.Background(), log, now); err != nil {
		t.Fatal(err)
	}
	if e := log.Add("a", "", "", nil); e.ID <= now.UnixMicro() {
		t.Errorf("id %d, want above %d", e.ID, now.UnixMicro())
	}
	w.start()
	log.Add("b", "", "", nil)
	w.stop()
	log.Add("c", "", "", nil) // after stop: in the log, not stored
	if got, _, _ := repo.RecentEvents(context.Background(), 10); len(got) != 2 || got[1].Type != "b" {
		t.Errorf("stored %+v, want a and b", got)
	}
	if n := log.Count(observability.EventFilter{}); n != 3 {
		t.Errorf("the log has %d events, want 3", n)
	}
	(&eventWriter{}).stop() // never started: nothing to do
}

// TestEventLogMaxIDAfterRestore: with no events after a restart, max id is
// 0 although new ids start after the time floor.
func TestEventLogMaxIDAfterRestore(t *testing.T) {
	log := observability.NewEventLog()
	w := newEventWriter(&countingEvents{MemStore: state.NewMemStore()}, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	now := time.Now()
	if err := w.restore(context.Background(), log, now); err != nil {
		t.Fatal(err)
	}
	if got := log.MaxID(); got != 0 {
		t.Errorf("max id with no events = %d, want 0", got)
	}
	e := log.Add("a", "", "", nil)
	if log.MaxID() != e.ID || e.ID <= now.UnixMicro() {
		t.Errorf("max id %d, event %d", log.MaxID(), e.ID)
	}
}

// TestEventWriterDrainDeadline: a store that hangs at shutdown does not
// hold stop beyond the drain limit.
func TestEventWriterDrainDeadline(t *testing.T) {
	var logs bytes.Buffer
	w := newEventWriter(hangingEvents{state.NewMemStore()}, slog.New(slog.NewTextHandler(&logs, nil)))
	w.every = time.Hour
	for i := range 3 * eventBatch {
		w.add(observability.Event{ID: int64(i + 1)})
	}
	w.start()
	started := time.Now()
	w.stop()
	if d := time.Since(started); d > eventDrainLimit+2*time.Second {
		t.Errorf("stop took %s", d)
	}
	if !strings.Contains(logs.String(), "events not stored") {
		t.Errorf("log = %s", logs.String())
	}
}

// hangingEvents never finishes a write before its context ends.
type hangingEvents struct{ *state.MemStore }

func (hangingEvents) AppendEvents(ctx context.Context, _ []state.EventRecord) error {
	<-ctx.Done()
	return ctx.Err()
}
