package pipeline

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/state"
)

type toggleSink struct{ fail bool }

func (s *toggleSink) Write(context.Context, Delivery) error {
	if s.fail {
		return errors.New("downstream unavailable")
	}
	return nil
}

func TestRetryDue(t *testing.T) {
	ctx := context.Background()
	store := state.NewMemStore()
	sink := &toggleSink{fail: true}
	obs := &recordingObserver{}
	p := New(store, func(Destination) (Sink, error) { return sink, nil }, obs, Options{MaxAttempts: 3, BackoffBase: time.Millisecond})
	f := Flow{ID: "f", Destinations: []Destination{{Name: "a", Type: "file", Dir: "d"}}}
	lookup := func(context.Context, string) (Flow, error) { return f, nil }

	res, err := p.Process(ctx, f, []byte("x"))
	if err != nil || res.Status != state.StatusQueued {
		t.Fatalf("first attempt = %+v, %v", res, err)
	}
	m, _ := store.Get(ctx, res.ID)
	if m.Attempts["a"].NextAttemptAt.IsZero() {
		t.Error("failed attempt did not schedule a retry")
	}

	time.Sleep(5 * time.Millisecond)
	if n, err := p.RetryDue(ctx, lookup); err != nil || n != 1 {
		t.Fatalf("retry pass 1 = %d, %v", n, err)
	}
	if m, _ := store.Get(ctx, res.ID); m.Status != state.StatusQueued || m.Attempts["a"].Attempts != 2 {
		t.Errorf("after failed retry: %+v", m)
	}

	sink.fail = false
	time.Sleep(5 * time.Millisecond)
	if _, err := p.RetryDue(ctx, lookup); err != nil {
		t.Fatal(err)
	}
	if m, _ := store.Get(ctx, res.ID); m.Status != state.StatusSent || m.Attempts["a"].Attempts != 3 {
		t.Errorf("after successful retry: %+v", m)
	}
	if last := obs.retried[len(obs.retried)-1]; last.Status != state.StatusSent {
		t.Errorf("observer saw %s", last.Status)
	}
	if n, _ := p.RetryDue(ctx, lookup); n != 0 {
		t.Errorf("sent message retried again (%d)", n)
	}
}

func TestRetryDueDeadLetters(t *testing.T) {
	ctx := context.Background()
	store := state.NewMemStore()
	p := New(store, func(Destination) (Sink, error) { return &toggleSink{fail: true}, nil }, nil, Options{MaxAttempts: 2, BackoffBase: time.Millisecond})
	f := Flow{ID: "f", Destinations: []Destination{{Name: "a", Type: "file", Dir: "d"}}}

	res, _ := p.Process(ctx, f, []byte("x"))
	time.Sleep(5 * time.Millisecond)
	if _, err := p.RetryDue(ctx, func(context.Context, string) (Flow, error) { return f, nil }); err != nil {
		t.Fatal(err)
	}
	if m, _ := store.Get(ctx, res.ID); m.Status != state.StatusDeadLettered {
		t.Errorf("status = %s, want dead-lettered after max attempts", m.Status)
	}

	// Not yet due: skipped.
	slow := New(store, func(Destination) (Sink, error) { return &toggleSink{fail: true}, nil }, nil, Options{BackoffBase: time.Hour})
	res2, _ := slow.Process(ctx, f, []byte("y"))
	if n, _ := slow.RetryDue(ctx, func(context.Context, string) (Flow, error) { return f, nil }); n != 0 {
		t.Errorf("retried a message that is not due (%d)", n)
	}

	// Flow deleted: dead-lettered with an explanation.
	store2 := state.NewMemStore()
	p2 := New(store2, func(Destination) (Sink, error) { return &toggleSink{fail: true}, nil }, nil, Options{BackoffBase: time.Millisecond})
	res3, _ := p2.Process(ctx, f, []byte("z"))
	time.Sleep(5 * time.Millisecond)
	if _, err := p2.RetryDue(ctx, func(context.Context, string) (Flow, error) { return Flow{}, ErrFlowGone }); err != nil {
		t.Fatal(err)
	}
	if m, _ := store2.Get(ctx, res3.ID); m.Status != state.StatusDeadLettered || m.Metadata["error"] == "" {
		t.Errorf("flow-gone message = %+v", m)
	}
	_ = res2

	// Lookup errors are returned.
	store3 := state.NewMemStore()
	p3 := New(store3, func(Destination) (Sink, error) { return &toggleSink{fail: true}, nil }, nil, Options{BackoffBase: time.Millisecond})
	_, _ = p3.Process(ctx, f, []byte("w"))
	time.Sleep(5 * time.Millisecond)
	if _, err := p3.RetryDue(ctx, func(context.Context, string) (Flow, error) { return Flow{}, errStore }); !errors.Is(err, errStore) {
		t.Errorf("lookup error = %v", err)
	}
}

func TestRetryDueStoreFailures(t *testing.T) {
	ctx := context.Background()
	f := Flow{ID: "f", Destinations: []Destination{{Name: "a", Type: "file", Dir: "d"}}}
	for n := 1; n <= 4; n++ {
		for _, failGet := range []bool{false, true} {
			mem := state.NewMemStore()
			seed := New(mem, func(Destination) (Sink, error) { return &toggleSink{fail: true}, nil }, nil, Options{BackoffBase: time.Millisecond})
			if _, err := seed.Process(ctx, f, []byte("x")); err != nil {
				t.Fatal(err)
			}
			time.Sleep(3 * time.Millisecond)
			store := &flakyStore{MemStore: mem}
			if failGet {
				store.failGet = n
			} else {
				store.failPut = n
			}
			p := New(store, func(Destination) (Sink, error) { return &toggleSink{}, nil }, nil, Options{BackoffBase: time.Millisecond})
			_, err := p.RetryDue(ctx, func(context.Context, string) (Flow, error) { return f, nil })
			calls := store.puts
			if failGet {
				calls = store.gets
			}
			if calls >= n && !errors.Is(err, errStore) {
				t.Errorf("failing call %d (get=%v): err = %v", n, failGet, err)
			}
		}
	}
}

// TestRetryResumesInterruptedMessages covers crash recovery: messages left
// in "received" or "transformed" (with an untried destination) are finished.
func TestRetryResumesInterruptedMessages(t *testing.T) {
	ctx := context.Background()
	store := state.NewMemStore()
	sink := &recordingSink{}
	p := New(store, func(Destination) (Sink, error) { return sink, nil }, nil, Options{BackoffBase: time.Millisecond})
	f := Flow{ID: "f", Transform: transform(t, "name: t\nsteps:\n  - set: { field: ok, expr: yes }"),
		Destinations: []Destination{{Name: "a", Type: "file", Dir: "d"}, {Name: "b", Type: "file", Dir: "d"}}}
	lookup := func(context.Context, string) (Flow, error) { return f, nil }

	_ = store.Put(ctx, state.Message{ID: "r", FlowID: "f", Status: state.StatusReceived, ContentType: "json", Raw: []byte(`{}`)})
	_ = store.Put(ctx, state.Message{ID: "t", FlowID: "f", Status: state.StatusTransformed, ContentType: "json", Transformed: []byte(`{"x":1}`),
		Attempts: map[string]state.DestinationAttempt{"a": {Attempts: 1}}})
	_ = store.Put(ctx, state.Message{ID: "q", FlowID: "f", Status: state.StatusQueued, ContentType: "json", Transformed: []byte(`{"y":1}`),
		Attempts: map[string]state.DestinationAttempt{"a": {Attempts: 1, LastError: "x"}}}) // b never tried

	n, err := p.RetryDue(ctx, lookup)
	if err != nil || n != 3 {
		t.Fatalf("RetryDue = %d, %v; want 3", n, err)
	}
	for _, id := range []string{"r", "t", "q"} {
		if m, _ := store.Get(ctx, id); m.Status != state.StatusSent {
			t.Errorf("%s: status %s, want sent (attempts %+v)", id, m.Status, m.Attempts)
		}
	}
	if m, _ := store.Get(ctx, "r"); string(m.Transformed) != `{"ok":"yes"}` {
		t.Errorf("received message not transformed: %s", m.Transformed)
	}
}

func TestRetrySkipsInFlightPausedAndContinuesPastErrors(t *testing.T) {
	ctx := context.Background()
	store := state.NewMemStore()
	p := New(store, func(Destination) (Sink, error) { return &recordingSink{}, nil }, nil, Options{BackoffBase: time.Millisecond})
	f := Flow{ID: "f", Destinations: []Destination{{Name: "a", Type: "file", Dir: "d"}}}
	for _, id := range []string{"1", "2", "3"} {
		_ = store.Put(ctx, state.Message{ID: id, FlowID: id, Status: state.StatusQueued, Transformed: []byte("x")})
	}
	p.inflight.Store("1", struct{}{}) // being processed by a request
	lookup := func(_ context.Context, flowID string) (Flow, error) {
		switch flowID {
		case "2":
			return Flow{}, errStore // lookup failure: skipped, error collected
		case "3":
			paused := f
			paused.Paused = true
			return paused, nil
		}
		return f, nil
	}
	n, err := p.RetryDue(ctx, lookup)
	if n != 0 || !errors.Is(err, errStore) {
		t.Errorf("RetryDue = %d, %v; want 0 resumed and the lookup error", n, err)
	}
	for _, id := range []string{"1", "2", "3"} {
		if m, _ := store.Get(ctx, id); m.Status != state.StatusQueued {
			t.Errorf("%s changed to %s", id, m.Status)
		}
	}

	// Cancellation stops the pass before any message.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if n, _ := p.RetryDue(cancelled, func(context.Context, string) (Flow, error) { return f, nil }); n != 0 {
		t.Errorf("cancelled pass resumed %d", n)
	}
}

func TestRetryPagesThroughAllQueued(t *testing.T) {
	ctx := context.Background()
	store := state.NewMemStore()
	p := New(store, func(Destination) (Sink, error) { return &recordingSink{}, nil }, nil, Options{})
	f := Flow{ID: "f", Destinations: []Destination{{Name: "a", Type: "file", Dir: "d"}}}
	total := retryPage*2 + 7
	for i := 0; i < total; i++ {
		_ = store.Put(ctx, state.Message{ID: fmt.Sprintf("%04d", i), FlowID: "f", Status: state.StatusQueued, Transformed: []byte("x")})
	}
	if n, err := p.RetryDue(ctx, func(context.Context, string) (Flow, error) { return f, nil }); err != nil || n != total {
		t.Errorf("RetryDue = %d, %v; want %d", n, err, total)
	}
}

// TestRetryRepairsStuckRollup: a crash between exhausting a destination and
// the rollup leaves "queued" with nothing to deliver; the retry pass fixes it.
func TestRetryRepairsStuckRollup(t *testing.T) {
	ctx := context.Background()
	store := state.NewMemStore()
	p := New(store, func(Destination) (Sink, error) { return &recordingSink{}, nil }, nil, Options{MaxAttempts: 2})
	f := Flow{ID: "f", Destinations: []Destination{{Name: "a", Type: "file", Dir: "d"}, {Name: "b", Type: "file", Dir: "d"}}}
	_ = store.Put(ctx, state.Message{ID: "m", FlowID: "f", Status: state.StatusQueued,
		Attempts: map[string]state.DestinationAttempt{"a": {Attempts: 2, LastError: "x"}, "b": {Attempts: 1}}})
	if n, err := p.RetryDue(ctx, func(context.Context, string) (Flow, error) { return f, nil }); err != nil || n != 1 {
		t.Fatalf("RetryDue = %d, %v", n, err)
	}
	if m, _ := store.Get(ctx, "m"); m.Status != state.StatusDeadLettered {
		t.Errorf("status = %s, want dead-lettered", m.Status)
	}
}
