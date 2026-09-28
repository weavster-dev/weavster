package outbox

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/state"
)

func msg(id string) state.Message {
	return state.Message{
		ID: id, FlowID: "f", Status: state.StatusReceived,
		ContentType: "hl7v2", Raw: []byte("MSH|...\r"),
	}
}

func TestIdempotencyKey(t *testing.T) {
	a := IdempotencyKey("m1", "d1")
	if a != IdempotencyKey("m1", "d1") {
		t.Error("key must be stable across retries of the same delivery (D-10)")
	}
	if a == IdempotencyKey("m1", "d2") || a == IdempotencyKey("m2", "d1") {
		t.Error("key must vary by message and destination")
	}
}

func TestSemanticsForAdapter(t *testing.T) {
	for adapter, want := range map[string]DeliverySemantics{
		"http":        SemanticsKeySent,
		"web-service": SemanticsAtLeastOnce,
		"tcp":         SemanticsAtLeastOnce,
		"mllp":        SemanticsAtLeastOnce,
		"file":        SemanticsAtLeastOnce,
		"smtp":        SemanticsAtLeastOnce,
		"document":    SemanticsAtLeastOnce,
		"interflow":   SemanticsKeySent,
		"database":    SemanticsAtLeastOnce, // the key only reaches the table with keyColumn
		"something":   SemanticsAtLeastOnce,
	} {
		if got := SemanticsForAdapter(adapter); got != want {
			t.Errorf("%s: %s, want %s", adapter, got, want)
		}
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

	stored, err := o.SetTransformed(ctx, "1", []byte("done"), "hl7v2", map[string]string{"k": "v"})
	if err != nil {
		t.Fatal(err)
	}
	m, _ = s.Get(ctx, "1")
	if m.Status != state.StatusTransformed || string(m.Transformed) != "done" || m.Metadata["k"] != "v" || stored.Metadata["k"] != "v" || m.ContentType != "hl7v2" {
		t.Errorf("after transform: %+v", m)
	}
	if _, err := o.SetTransformed(ctx, "1", []byte("again"), "", nil); err != nil {
		t.Fatal(err)
	}
	if m, _ = s.Get(ctx, "1"); string(m.Transformed) != "again" || m.Metadata["k"] != "v" || m.ContentType != "hl7v2" {
		t.Errorf("metadata not kept: %+v", m)
	}
	if _, err := o.SetTransformed(ctx, "1", []byte("again"), "", map[string]string{"k": ""}); err != nil {
		t.Fatal(err)
	}
	if m, _ = s.Get(ctx, "1"); m.Metadata["k"] != "" {
		t.Errorf("empty value did not remove the key: %+v", m)
	}
}

func TestSetTransformedMissingMessage(t *testing.T) {
	o := New(state.NewMemStore(), nil, Options{})
	if _, err := o.SetTransformed(context.Background(), "missing", []byte("x"), "", nil); err == nil {
		t.Fatal("SetTransformed() on a missing message = nil, want an error")
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
	if m.Status != state.StatusReceived {
		t.Errorf("status = %s; Deliver must leave the aggregate status to the caller", m.Status)
	}
	if m.Attempts["d1"].Attempts != 1 || m.Attempts["d1"].LastError != "" {
		t.Errorf("attempts = %+v", m.Attempts)
	}
	if gotKey != IdempotencyKey("1", "d1") {
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
		// A failed delivery is recorded, not returned: only store errors are.
		if err := o.Deliver(ctx, "1", "d1"); err != nil {
			t.Fatalf("Deliver returned the sink error: %v", err)
		}
	}
	m, _ := s.Get(ctx, "1")
	if m.Status != state.StatusQueued || !m.Attempts["d1"].NextAttemptAt.IsZero() {
		t.Errorf("status = %s, next = %v; want queued with the destination exhausted", m.Status, m.Attempts["d1"].NextAttemptAt)
	}
	if m.Attempts["d1"].Attempts != 3 {
		t.Errorf("attempts = %d, want 3", m.Attempts["d1"].Attempts)
	}
	if atomic.LoadInt32(&calls) != 3 {
		t.Errorf("deliver calls = %d, want 3", calls)
	}

	// Dead-letter surface: the caller (the pipeline's rollup) sets the status.
	m.Status = state.StatusDeadLettered
	_ = s.Put(ctx, m)
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
	if m.Status != state.StatusQueued || m.Attempts["d1"].LastError != "" {
		t.Errorf("status = %s, attempts = %+v; want the delivery recorded and the status left to the caller", m.Status, m.Attempts)
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

// TestDeliverRecordsCode: a failed attempt records its code (from an
// error's Code(), or the ErrorCode option) and when it ended; a success
// clears the code.
func TestDeliverRecordsCode(t *testing.T) {
	ctx := context.Background()
	s := state.NewMemStore()
	_ = s.Put(ctx, msg("1"))
	fail := true
	o := New(s, func(context.Context, state.Message, string, string) error {
		if fail {
			return coded{"http:503"}
		}
		return nil
	}, Options{BackoffBase: time.Millisecond})
	before := time.Now()
	_ = o.Deliver(ctx, "1", "d")
	m, _ := s.Get(ctx, "1")
	if a := m.Attempts["d"]; a.LastCode != "http:503" || a.LastAttemptAt.Before(before) {
		t.Errorf("after a failure: %+v", a)
	}
	fail = false
	_ = o.Deliver(ctx, "1", "d")
	m, _ = s.Get(ctx, "1")
	if a := m.Attempts["d"]; a.LastCode != "" || a.LastError != "" || a.LastAttemptAt.IsZero() {
		t.Errorf("after a success: %+v", a)
	}
	custom := New(s, func(context.Context, state.Message, string, string) error { return errors.New("x") },
		Options{ErrorCode: func(error) string { return "net:refused" }})
	_ = custom.Deliver(ctx, "1", "e")
	if m, _ = s.Get(ctx, "1"); m.Attempts["e"].LastCode != "net:refused" {
		t.Errorf("ErrorCode option: %+v", m.Attempts["e"])
	}
}

type coded struct{ code string }

func (c coded) Error() string { return "failed" }
func (c coded) Code() string  { return c.code }
