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

func TestAmbiguousStatusCheckFailureDoesNotResend(t *testing.T) {
	s := state.NewMemStore()
	ctx := context.Background()
	m := msg("1")
	m.Status = state.StatusQueued
	m.Attempts = map[string]state.DestinationAttempt{
		"d1": {Attempts: 1, LastError: ErrAmbiguous.Error()},
	}
	if err := s.Put(ctx, m); err != nil {
		t.Fatal(err)
	}

	statusErr := errors.New("status unavailable")
	var deliverCalls int32
	o := New(s, func(context.Context, state.Message, string, string) error {
		atomic.AddInt32(&deliverCalls, 1)
		return nil
	}, Options{CheckStatus: func(context.Context, state.Message, string) (bool, error) {
		return false, statusErr
	}})

	if err := o.Deliver(ctx, "1", "d1"); !errors.Is(err, statusErr) {
		t.Fatalf("Deliver() error = %v, want %v", err, statusErr)
	}
	if got := atomic.LoadInt32(&deliverCalls); got != 0 {
		t.Errorf("deliver called %d times, want 0", got)
	}

	got, err := s.Get(ctx, "1")
	if err != nil {
		t.Fatal(err)
	}
	if attempt := got.Attempts["d1"]; attempt.Attempts != 1 || attempt.LastError != ErrAmbiguous.Error() {
		t.Errorf("attempt changed after status failure: %+v", attempt)
	}
	if got.Status != state.StatusQueued {
		t.Errorf("status = %s, want queued", got.Status)
	}
}

func TestAmbiguousNotDeliveredRetriesWithNextIdempotencyKey(t *testing.T) {
	s := state.NewMemStore()
	ctx := context.Background()
	m := msg("1")
	m.Status = state.StatusQueued
	m.Attempts = map[string]state.DestinationAttempt{
		"d1": {Attempts: 1, LastError: ErrAmbiguous.Error()},
	}
	if err := s.Put(ctx, m); err != nil {
		t.Fatal(err)
	}

	var gotKey string
	o := New(s, func(_ context.Context, _ state.Message, _ string, key string) error {
		gotKey = key
		return nil
	}, Options{CheckStatus: func(context.Context, state.Message, string) (bool, error) {
		return false, nil
	}})

	if err := o.Deliver(ctx, "1", "d1"); err != nil {
		t.Fatal(err)
	}
	if want := IdempotencyKey("1", "d1", 2); gotKey != want {
		t.Errorf("idempotency key = %q, want %q", gotKey, want)
	}

	got, err := s.Get(ctx, "1")
	if err != nil {
		t.Fatal(err)
	}
	if attempt := got.Attempts["d1"]; attempt.Attempts != 2 || attempt.LastError != "" {
		t.Errorf("attempt after retry: %+v", attempt)
	}
	if got.Status != state.StatusSent {
		t.Errorf("status = %s, want sent", got.Status)
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
