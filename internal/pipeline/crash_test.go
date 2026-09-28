package pipeline

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/state"
)

// crashStore is a store that dies at its nth write: that write and every
// store call after it fail, as if the server had stopped there.
type crashStore struct {
	*state.MemStore
	mu     sync.Mutex
	puts   int
	crash  int
	crashd bool
}

func (s *crashStore) dead() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.crashd
}

func (s *crashStore) Put(ctx context.Context, m state.Message) error {
	s.mu.Lock()
	s.puts++
	if s.puts >= s.crash {
		s.crashd = true
	}
	s.mu.Unlock()
	if s.dead() {
		return errStore
	}
	return s.MemStore.Put(ctx, m)
}

func (s *crashStore) Get(ctx context.Context, id string) (state.Message, error) {
	if s.dead() {
		return state.Message{}, errStore
	}
	return s.MemStore.Get(ctx, id)
}

func (s *crashStore) Search(ctx context.Context, q state.Query) ([]state.Message, error) {
	if s.dead() {
		return nil, errStore
	}
	return s.MemStore.Search(ctx, q)
}

// keySink records each destination's delivered idempotency keys.
type keySink struct {
	mu   sync.Mutex
	keys map[string][]string // destination -> keys delivered
}

// TestCrashAtEveryWrite: the server stops at each of a message's writes in
// turn (received, transformed, each destination's result, the final
// status). A message whose first write failed was never acknowledged and
// is not stored; any other is finished by the recovery pass of the next
// start: sent to both destinations, each delivery with the one idempotency
// key of that message and destination, and repeated at most once (only
// when the stop fell between a delivery and its record).
func TestCrashAtEveryWrite(t *testing.T) {
	f := Flow{ID: "f", Transform: transform(t, "name: t\nsteps:\n  - set: { field: ok, expr: yes }"),
		Destinations: []Destination{{Name: "a", Type: "file", Dir: "d"}, {Name: "b", Type: "file", Dir: "d"}}}
	lookup := func(context.Context, string) (Flow, error) { return f, nil }
	crashes := 0
	for crash := 1; crash <= 8; crash++ {
		ctx := context.Background()
		mem := state.NewMemStore()
		sink := &keySink{keys: map[string][]string{}}
		build := func(d Destination) (Sink, error) {
			return &destSink{name: d.Name, sink: sink}, nil
		}
		store := &crashStore{MemStore: mem, crash: crash}
		res, err := New(store, build, nil, Options{BackoffBase: time.Millisecond}).Process(ctx, f, []byte(`{}`))
		if !store.dead() {
			if err != nil || res.Status != state.StatusSent {
				t.Fatalf("crash at write %d never came: %+v %v", crash, res, err)
			}
			continue // the message needs fewer writes
		}
		crashes++
		stored, _ := mem.Search(ctx, state.Query{})
		if len(stored) == 0 {
			if err == nil || crash != 1 {
				t.Errorf("crash at write %d: nothing stored, err %v; only the first write may lose the message, and only unacknowledged", crash, err)
			}
			continue
		}
		// The next start: the store is healthy, the recovery pass runs.
		p := New(mem, build, nil, Options{BackoffBase: time.Millisecond})
		for i := 0; i < 5; i++ {
			if _, err := p.RetryDue(ctx, lookup); err != nil {
				t.Fatalf("crash at write %d: recovery: %v", crash, err)
			}
			time.Sleep(2 * time.Millisecond)
		}
		m, _ := mem.Get(ctx, stored[0].ID)
		if m.Status != state.StatusSent {
			t.Errorf("crash at write %d: after recovery the message is %s, want sent (%+v)", crash, m.Status, m.Attempts)
		}
		for _, dest := range []string{"a", "b"} {
			keys := sink.keys[dest]
			if len(keys) < 1 || len(keys) > 2 {
				t.Errorf("crash at write %d: %s delivered %d times, want 1 (2 only after a stop between delivery and record)", crash, dest, len(keys))
			}
			for _, k := range keys {
				if k != keys[0] {
					t.Errorf("crash at write %d: %s got different idempotency keys %v", crash, dest, keys)
				}
			}
		}
	}
}

// destSink records deliveries under its destination's name.
type destSink struct {
	name string
	sink *keySink
}

func (s *destSink) Write(_ context.Context, d Delivery) error {
	return s.sink.writeAs(s.name, d)
}

func (s *keySink) writeAs(dest string, d Delivery) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[dest] = append(s.keys[dest], d.IdempotencyKey)
	return nil
}
