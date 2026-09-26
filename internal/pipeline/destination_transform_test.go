package pipeline

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/compiler"
	"github.com/weavster-dev/weavster/internal/state"
)

func steps(s ...compiler.Step) *compiler.Transform {
	return &compiler.Transform{Name: "t", Steps: s}
}

func setField(field, expr string) compiler.Step {
	return compiler.Step{Set: &compiler.SetStep{Field: field, Expr: expr}}
}

func rejectWhen(when string) compiler.Step {
	return compiler.Step{Filter: &compiler.FilterStep{When: when, Action: "reject"}}
}

func TestDestinationTransforms(t *testing.T) {
	tag := steps(setField("to", "b"))
	dropAll := steps(rejectWhen("kind == 'adt'"))
	toNumber := steps(compiler.Step{Map: &compiler.MapStep{From: "kind", To: "n", Type: "number"}})
	tests := []struct {
		name       string
		flow       *compiler.Transform
		a, b       *compiler.Transform
		body       string
		wantStatus state.Status
		wantA      []string
		wantB      []string
	}{
		{"b gets its own output", nil, nil, tag, `{"kind":"adt"}`, state.StatusSent,
			[]string{`{"kind":"adt"}`}, []string{`{"kind":"adt","to":"b"}`}},
		{"b filters, a still delivered", nil, nil, dropAll, `{"kind":"adt"}`, state.StatusSent,
			[]string{`{"kind":"adt"}`}, nil},
		{"every destination filters", nil, dropAll, dropAll, `{"kind":"adt"}`, state.StatusFiltered, nil, nil},
		{"filter not matched", nil, dropAll, nil, `{"kind":"orm"}`, state.StatusSent,
			[]string{`{"kind":"orm"}`}, []string{`{"kind":"orm"}`}},
		{"runs on the flow output", steps(setField("kind", "orm")), dropAll, nil, `{"kind":"adt"}`, state.StatusSent,
			[]string{`{"kind":"orm"}`}, []string{`{"kind":"orm"}`}},
		{"failing destination transform queues", nil, toNumber, nil, `{"kind":"adt"}`, state.StatusQueued,
			nil, []string{`{"kind":"adt"}`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := state.NewMemStore()
			sa, sb := &recordingSink{}, &recordingSink{}
			p := New(store, func(d Destination) (Sink, error) {
				if d.Name == "a" {
					return sa, nil
				}
				return sb, nil
			}, nil, Options{})
			f := Flow{ID: "f", Transform: tt.flow, Destinations: []Destination{
				{Name: "a", Type: "file", Dir: "x", Transform: tt.a},
				{Name: "b", Type: "file", Dir: "y", Transform: tt.b},
			}}
			if err := Validate(f); err != nil {
				t.Fatal(err)
			}
			res, err := p.Process(ctx, f, []byte(tt.body))
			if err != nil || res.Status != tt.wantStatus {
				t.Fatalf("Process = %+v, %v; want %s", res, err, tt.wantStatus)
			}
			if !slices.Equal(sa.bodies, tt.wantA) || !slices.Equal(sb.bodies, tt.wantB) {
				t.Errorf("a got %q, b got %q; want %q and %q", sa.bodies, sb.bodies, tt.wantA, tt.wantB)
			}
		})
	}
}

// TestDestinationTransformRetries: a failing destination transform
// exhausts that destination only, and a retry pass never delivers to a
// destination that filtered the message.
func TestDestinationTransformRetries(t *testing.T) {
	ctx := context.Background()
	store := state.NewMemStore()
	sink := &recordingSink{}
	p := New(store, func(Destination) (Sink, error) { return sink, nil }, nil, Options{MaxAttempts: 2, BackoffBase: time.Millisecond})
	f := Flow{ID: "f", Destinations: []Destination{
		{Name: "bad", Type: "file", Dir: "x", Transform: steps(compiler.Step{Map: &compiler.MapStep{From: "kind", To: "n", Type: "number"}})},
		{Name: "skip", Type: "file", Dir: "y", Transform: steps(rejectWhen("kind"))},
		{Name: "ok", Type: "file", Dir: "z"},
	}}
	lookup := func(context.Context, string) (Flow, error) { return f, nil }
	res, err := p.Process(ctx, f, []byte(`{"kind":"adt"}`))
	if err != nil || res.Status != state.StatusQueued {
		t.Fatalf("Process = %+v, %v", res, err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := p.RetryDue(ctx, lookup); err != nil {
		t.Fatal(err)
	}
	m, _ := store.Get(ctx, res.ID)
	if m.Status != state.StatusDeadLettered || m.Attempts["bad"].Attempts != 2 || m.Attempts["skip"].Attempts != 0 {
		t.Errorf("after retries: status %s, attempts %+v", m.Status, m.Attempts)
	}
	if len(sink.bodies) != 1 {
		t.Errorf("deliveries = %q, want only ok's", sink.bodies)
	}
}

func TestValidateDestinationTransform(t *testing.T) {
	f := Flow{Destinations: []Destination{{Name: "a", Type: "file", Dir: "x", Transform: steps(compiler.Step{})}}}
	if err := Validate(f); err == nil {
		t.Error("Validate accepted a destination transform with an empty step")
	}
	// A missing name is reported before the transform.
	f.Destinations[0].Name = ""
	if err := Validate(f); err == nil || !strings.Contains(err.Error(), "destinations[0]: name is required") {
		t.Errorf("Validate = %v, want the missing name reported", err)
	}
}

// TestDestinationTransformNeedsObject: with no flow transform, a message
// for a flow with a destination transform must still be a JSON object.
func TestDestinationTransformNeedsObject(t *testing.T) {
	p := New(state.NewMemStore(), func(Destination) (Sink, error) { return &recordingSink{}, nil }, nil, Options{})
	f := Flow{ID: "f", Destinations: []Destination{{Name: "a", Type: "file", Dir: "x", Transform: steps(setField("x", "y"))}}}
	if _, err := p.Process(context.Background(), f, []byte("MSH|")); !errors.Is(err, ErrInvalidMessage) {
		t.Errorf("Process = %v, want ErrInvalidMessage", err)
	}
}

// TestFilterAfterDelivery: a destination that already received the message
// stays delivered when a later definition would filter it, so the message
// is sent, not filtered.
func TestFilterAfterDelivery(t *testing.T) {
	ctx := context.Background()
	store := state.NewMemStore()
	p := New(store, func(Destination) (Sink, error) { return &recordingSink{}, nil }, nil, Options{})
	_ = store.Put(ctx, state.Message{ID: "m", FlowID: "f", Status: state.StatusQueued, Transformed: []byte(`{"kind":"adt"}`),
		Attempts: map[string]state.DestinationAttempt{"a": {Attempts: 1}, "b": {Attempts: 1, LastError: "down"}}})
	drop := steps(rejectWhen("kind"))
	f := Flow{ID: "f", Destinations: []Destination{{Name: "a", Type: "file", Dir: "x", Transform: drop}, {Name: "b", Type: "file", Dir: "y", Transform: drop}}}
	if _, err := p.RetryDue(ctx, func(context.Context, string) (Flow, error) { return f, nil }); err != nil {
		t.Fatal(err)
	}
	if m, _ := store.Get(ctx, "m"); m.Status != state.StatusSent {
		t.Errorf("status = %s, want sent (a received it)", m.Status)
	}
}

// TestStoppedDestinationHoldsFilteredMessage: a stopped destination whose
// filter would drop the message still holds it queued until started.
func TestStoppedDestinationHoldsFilteredMessage(t *testing.T) {
	ctx := context.Background()
	store := state.NewMemStore()
	p := New(store, func(Destination) (Sink, error) { return &recordingSink{}, nil }, nil, Options{})
	f := Flow{ID: "f", Destinations: []Destination{
		{Name: "a", Type: "file", Dir: "x"},
		{Name: "b", Type: "file", Dir: "y", Transform: steps(rejectWhen("kind")), Stopped: true},
	}}
	res, err := p.Process(ctx, f, []byte(`{"kind":"adt"}`))
	if err != nil || res.Status != state.StatusQueued {
		t.Fatalf("Process = %+v, %v; want queued while b is stopped", res, err)
	}
	f.Destinations[1].Stopped = false
	if _, err := p.RetryDue(ctx, func(context.Context, string) (Flow, error) { return f, nil }); err != nil {
		t.Fatal(err)
	}
	if m, _ := store.Get(ctx, res.ID); m.Status != state.StatusSent {
		t.Errorf("after starting b: %s, want sent (b filtered it)", m.Status)
	}
}
