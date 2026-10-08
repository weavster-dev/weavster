package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/state"
)

// countingSink counts writes and fails while fail is set.
type countingSink struct {
	fail   bool
	writes int
}

func (s *countingSink) Write(context.Context, Delivery) error {
	s.writes++
	if s.fail {
		return errors.New("downstream unavailable")
	}
	return nil
}

func TestRequeue(t *testing.T) {
	ctx := context.Background()
	store := state.NewMemStore()
	good, bad := &countingSink{}, &countingSink{fail: true}
	sinks := map[string]*countingSink{"good": good, "bad": bad}
	p := New(store, func(d Destination) (Sink, error) { return sinks[d.Name], nil }, nil, Options{MaxAttempts: 2, BackoffBase: time.Millisecond})
	f := Flow{ID: "f", Destinations: []Destination{{Name: "good", Type: "file", Dir: "g"}, {Name: "bad", Type: "file", Dir: "b"}}}
	lookup := func(context.Context, string) (Flow, error) { return f, nil }

	res, _ := p.Process(ctx, f, []byte("x"))
	time.Sleep(5 * time.Millisecond)
	if _, err := p.RetryDue(ctx, lookup); err != nil {
		t.Fatal(err)
	}
	if m, _ := store.Get(ctx, res.ID); m.Status != state.StatusDeadLettered {
		t.Fatalf("status = %s, want dead-lettered", m.Status)
	}

	// Requeue: the exhausted destination starts again, the delivered one
	// keeps its record, and the previous state is returned.
	before, err := p.Requeue(ctx, res.ID)
	if err != nil || before.Attempts["bad"].Attempts != 2 || before.Attempts["bad"].LastError == "" || before.Status != state.StatusDeadLettered {
		t.Fatalf("requeue = %+v, %v", before, err)
	}
	m, _ := store.Get(ctx, res.ID)
	if m.Status != state.StatusQueued || m.Attempts["good"].Attempts != 1 || m.Attempts["bad"] != (state.DestinationAttempt{}) || m.Metadata["requeues"] != "1" {
		t.Errorf("after requeue = %+v", m)
	}

	// The retry pass delivers to the failed destination only.
	bad.fail = false
	if _, err := p.RetryDue(ctx, lookup); err != nil {
		t.Fatal(err)
	}
	if m, _ := store.Get(ctx, res.ID); m.Status != state.StatusSent || good.writes != 1 || bad.writes != 3 {
		t.Errorf("after retry: %+v, good %d, bad %d writes", m, good.writes, bad.writes)
	}

	for _, tt := range []struct {
		name, id string
		hold     bool
		want     error
	}{
		{"not dead-lettered", res.ID, false, ErrNotDeadLettered},
		{"unknown", "nope", false, state.ErrNotFound},
		{"in flight", res.ID, true, ErrInFlight},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.hold {
				release, _ := p.Hold(tt.id)
				defer release()
			}
			if _, err := p.Requeue(ctx, tt.id); !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}

	// A flow-deleted message loses its error on requeue and counts on.
	if err := store.Put(ctx, state.Message{ID: "gone", FlowID: "f", Status: state.StatusDeadLettered, Metadata: map[string]string{"error": "flow gone", "requeues": "2", "k": "v"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Requeue(ctx, "gone"); err != nil {
		t.Fatal(err)
	}
	// Never transformed (its flow was deleted first): back to received, so
	// the retry pass transforms it before delivering.
	if m, _ := store.Get(ctx, "gone"); m.Metadata["error"] != "" || m.Metadata["requeues"] != "3" || m.Metadata["k"] != "v" || m.Status != state.StatusReceived {
		t.Errorf("never-transformed message after requeue = %+v", m)
	}
}

func TestRemoveDeadLettered(t *testing.T) {
	ctx := context.Background()
	store := state.NewMemStore()
	p := New(store, nil, nil, Options{})
	for id, st := range map[string]state.Status{"dl": state.StatusDeadLettered, "q": state.StatusQueued, "busy": state.StatusDeadLettered} {
		if err := store.Put(ctx, state.Message{ID: id, FlowID: "f", Status: st}); err != nil {
			t.Fatal(err)
		}
	}
	release, _ := p.Hold("busy")
	defer release()
	for _, tt := range []struct {
		id   string
		want error
	}{
		{"q", ErrNotDeadLettered},
		{"busy", ErrInFlight},
		{"nope", state.ErrNotFound},
		{"dl", nil},
	} {
		if err := p.RemoveDeadLettered(ctx, tt.id); !errors.Is(err, tt.want) {
			t.Errorf("%s: %v, want %v", tt.id, err, tt.want)
		}
	}
	if _, err := store.Get(ctx, "dl"); !errors.Is(err, state.ErrNotFound) {
		t.Errorf("dl still stored: %v", err)
	}
	if _, err := store.Get(ctx, "q"); err != nil {
		t.Errorf("queued message removed: %v", err)
	}
}
