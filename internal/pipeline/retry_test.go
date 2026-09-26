package pipeline

import (
	"context"
	"errors"
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
