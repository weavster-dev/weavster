package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/state"
)

// TestCrashAtEveryWrite: the server stops at each of a message's writes in
// turn, on the delivered, the queued (a failing destination), and the
// filtered path. A message whose first write failed was not stored and its
// sender got an error; any other was acknowledged (its id returned) and is
// finished by the next start's recovery pass: every destination receives
// it with the one idempotency key of that message and destination, and a
// destination receives it twice only when the stop fell on the write that
// records its delivery.
func TestCrashAtEveryWrite(t *testing.T) {
	two := []Destination{{Name: "a", Type: "file", Dir: "d"}, {Name: "b", Type: "file", Dir: "d"}}
	for _, tt := range []struct {
		name   string
		flow   Flow
		failB  bool           // b fails its first attempt: the message is queued
		repeat map[int]string // crash write -> the destination delivered again
		final  state.Status
		writes int // the writes the message needs without a crash
	}{
		// Writes: 1 received, 2 transformed, 3 a's result, 4 b's result, 5 status.
		{name: "delivered", flow: Flow{ID: "f", Transform: transform(t, "name: t\nsteps:\n  - set: { field: ok, expr: yes }"), Destinations: two},
			repeat: map[int]string{3: "a", 4: "b"}, final: state.StatusSent, writes: 5},
		{name: "queued", flow: Flow{ID: "f", Transform: transform(t, "name: t\nsteps:\n  - set: { field: ok, expr: yes }"), Destinations: two},
			failB: true, repeat: map[int]string{3: "a"}, final: state.StatusSent, writes: 5},
		{name: "filtered", flow: Flow{ID: "f", Transform: transform(t, "name: t\nsteps:\n  - filter: { when: missing, action: accept }"), Destinations: two},
			final: state.StatusFiltered, writes: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			lookup := func(context.Context, string) (Flow, error) { return tt.flow, nil }
			covered := 0
			for crash := 1; crash <= tt.writes+1; crash++ {
				ctx := context.Background()
				mem := state.NewMemStore()
				sinks := map[string]*recordingSink{"a": {}, "b": {}}
				if tt.failB {
					sinks["b"].fail = errors.New("b is down")
				}
				build := func(d Destination) (Sink, error) { return sinks[d.Name], nil }
				store := &flakyStore{MemStore: mem, failPut: crash, sticky: true}
				res, err := New(store, build, nil, Options{BackoffBase: time.Millisecond}).Process(ctx, tt.flow, []byte(`{}`))
				if !store.down {
					if crash <= tt.writes {
						t.Fatalf("crash at write %d did not come: the message needs fewer writes than %d", crash, tt.writes)
					}
					continue // every write point was covered
				}
				covered++
				stored, _ := mem.Search(ctx, state.Query{})
				if crash == 1 {
					if len(stored) != 0 || err == nil || res.ID != "" {
						t.Errorf("crash at the first write: stored %d, %+v, %v; want nothing stored and an error", len(stored), res, err)
					}
					continue
				}
				if len(stored) != 1 || res.ID != stored[0].ID {
					t.Fatalf("crash at write %d: stored %d messages, result %+v; want the stored id acknowledged", crash, len(stored), res)
				}

				// The next start: the store is healthy, b is back, one recovery pass.
				sinks["b"].fail = nil
				time.Sleep(2 * time.Millisecond) // past any backoff
				if _, err := New(mem, build, nil, Options{BackoffBase: time.Millisecond}).RetryDue(ctx, lookup); err != nil {
					t.Fatalf("crash at write %d: recovery: %v", crash, err)
				}
				m, _ := mem.Get(ctx, stored[0].ID)
				if m.Status != tt.final {
					t.Errorf("crash at write %d: after recovery %s, want %s (%+v)", crash, m.Status, tt.final, m.Attempts)
				}
				for dest, sink := range sinks {
					want := 1
					switch {
					case tt.final == state.StatusFiltered:
						want = 0
					case tt.repeat[crash] == dest:
						want = 2
					}
					if len(sink.keys) != want {
						t.Errorf("crash at write %d: %s delivered %d times, want %d", crash, dest, len(sink.keys), want)
					}
					for _, k := range sink.keys {
						if k != sink.keys[0] {
							t.Errorf("crash at write %d: %s got different idempotency keys %v", crash, dest, sink.keys)
						}
					}
				}
			}
			if covered != tt.writes {
				t.Errorf("%d of %d write points crashed", covered, tt.writes)
			}
		})
	}
}
