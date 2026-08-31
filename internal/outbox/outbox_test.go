package outbox

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/weavster-dev/weavster/internal/state"
)

func msg(id string) state.Message {
	return state.Message{
		ID: id, FlowID: "f", Status: state.StatusReceived,
		ContentType: "hl7v2", Raw: []byte("MSH|...\r"),
	}
}

func TestIdempotencyKey(t *testing.T) {
	a := IdempotencyKey("m1", "d1", 1)
	b := IdempotencyKey("m1", "d1", 1)
	if a != b {
		t.Error("key must be deterministic")
	}
	if a == IdempotencyKey("m1", "d1", 2) || a == IdempotencyKey("m1", "d2", 1) || a == IdempotencyKey("m2", "d1", 1) {
		t.Error("key must vary by message, destination, and attempt")
	}
}

func TestSemanticsForAdapter(t *testing.T) {
	if SemanticsForAdapter("tcp") != SemanticsAtLeastOnce {
		t.Error("raw tcp mllp must be at-least-once")
	}
	if SemanticsForAdapter("http") != SemanticsExactlyOnce {
		t.Error("http must be exactly-once")
	}
}

func TestReceiveAndTransform(t *testing.T) {
	s := state.NewMemStore()
	o := New(s, func(context.Context, state.Message, string, string) error { return nil }, Options{})
	ctx := context.Background()

	if err := o.Receive(ctx, msg("1")); err != nil {
		t.Fatal(err)
	}
	m, _ := s.Get(ctx, "1")
	if m.Status != state.StatusReceived {
		t.Errorf("status = %s", m.Status)
	}

	if err := o.Transform(ctx, "1", func(b []byte) ([]byte, error) { return []byte("done"), nil }); err != nil {
		t.Fatal(err)
	}
	m, _ = s.Get(ctx, "1")
	if m.Status != state.StatusTransformed || string(m.Transformed) != "done" {
		t.Errorf("after transform: %+v", m)
	}
}

func TestTransformPropagatesFailuresWithoutPersistingChanges(t *testing.T) {
	ctx := context.Background()
	s := state.NewMemStore()
	o := New(s, nil, Options{})

	if err := o.Transform(ctx, "missing", func([]byte) ([]byte, error) {
		return []byte("unexpected"), nil
	}); err == nil {
		t.Fatal("Transform() missing message error = nil, want non-nil")
	}

	original := msg("1")
	if err := s.Put(ctx, original); err != nil {
		t.Fatal(err)
	}
	transformErr := errors.New("transform failed")
	if err := o.Transform(ctx, "1", func([]byte) ([]byte, error) {
		return nil, transformErr
	}); !errors.Is(err, transformErr) {
		t.Errorf("Transform() error = %v, want %v", err, transformErr)
	}

	got, err := s.Get(ctx, "1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != original.Status || string(got.Transformed) != string(original.Transformed) {
		t.Errorf("message persisted after transform failure: %+v", got)
	}
}

func TestDeliverSuccess(t *testing.T) {
	s := state.NewMemStore()
	ctx := context.Background()
	_ = s.Put(ctx, msg("1"))

	var gotKey string
	o := New(s, func(_ context.Context, _ state.Message, _ string, key string) error {
		gotKey = key
		return nil
	}, Options{})

	if err := o.Deliver(ctx, "1", "d1"); err != nil {
		t.Fatal(err)
	}
	m, _ := s.Get(ctx, "1")
	if m.Status != state.StatusSent {
		t.Errorf("status = %s, want sent", m.Status)
	}
	if m.Attempts["d1"].Attempts != 1 || m.Attempts["d1"].LastError != "" {
		t.Errorf("attempts = %+v", m.Attempts)
	}
	if gotKey != IdempotencyKey("1", "d1", 1) {
		t.Errorf("idempotency key = %q", gotKey)
	}
}

func TestDeliverBoundedRetryAndDeadLetter(t *testing.T) {
	s := state.NewMemStore()
	ctx := context.Background()
	_ = s.Put(ctx, msg("1"))

	var calls int32
	boom := errors.New("connection refused")
	o := New(s, func(context.Context, state.Message, string, string) error {
		atomic.AddInt32(&calls, 1)
		return boom
	}, Options{MaxAttempts: 3})

	for i := 0; i < 3; i++ {
		_ = o.Deliver(ctx, "1", "d1")
	}
	m, _ := s.Get(ctx, "1")
	if m.Status != state.StatusErrored {
		t.Errorf("status = %s, want errored (dead-letter)", m.Status)
	}
	if m.Attempts["d1"].Attempts != 3 {
		t.Errorf("attempts = %d, want 3", m.Attempts["d1"].Attempts)
	}
	if atomic.LoadInt32(&calls) != 3 {
		t.Errorf("deliver calls = %d, want 3", calls)
	}

	// Dead-letter surface.
	dl, err := o.DeadLetter(ctx)
	if err != nil || len(dl) != 1 {
		t.Errorf("deadletter = %d results, err %v", len(dl), err)
	}

	// Requeue resets and allows retry.
	if err := o.Requeue(ctx, "1"); err != nil {
		t.Fatal(err)
	}
	m, _ = s.Get(ctx, "1")
	if m.Status != state.StatusQueued || m.Attempts["d1"].Attempts != 0 {
		t.Errorf("after requeue: %+v", m)
	}
}

func TestAmbiguousChecksStatusFirst(t *testing.T) {
	s := state.NewMemStore()
	ctx := context.Background()
	_ = s.Put(ctx, msg("1"))

	var deliverCalls int32
	o := New(s, func(context.Context, state.Message, string, string) error {
		atomic.AddInt32(&deliverCalls, 1)
		return ErrAmbiguous
	}, Options{MaxAttempts: 5, CheckStatus: func(context.Context, state.Message, string) (bool, error) {
		return true, nil // downstream actually received it
	}})

	// First delivery: ambiguous outcome.
	_ = o.Deliver(ctx, "1", "d1")
	m, _ := s.Get(ctx, "1")
	if m.Status != state.StatusQueued || m.Attempts["d1"].LastError != ErrAmbiguous.Error() {
		t.Errorf("after ambiguous: %+v", m)
	}

	// Second delivery: check status first, must NOT re-send.
	if err := o.Deliver(ctx, "1", "d1"); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&deliverCalls) != 1 {
		t.Errorf("deliver called %d times, want 1 (status check must avoid re-send)", deliverCalls)
	}
	m, _ = s.Get(ctx, "1")
	if m.Status != state.StatusSent {
		t.Errorf("status = %s, want sent", m.Status)
	}
}

func TestBackoff(t *testing.T) {
	o := New(state.NewMemStore(), nil, Options{BackoffBase: 100})
	if o.Backoff(2) <= o.Backoff(1) {
		t.Error("backoff must grow with attempts")
	}
	if o.Backoff(1) != 100 {
		t.Errorf("backoff(1) = %v", o.Backoff(1))
	}
	if o.Backoff(2) != 200 {
		t.Errorf("backoff(2) = %v", o.Backoff(2))
	}
}
