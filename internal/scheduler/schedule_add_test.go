package scheduler

import (
	"context"
	"testing"
	"time"
)

// TestSchedulerAddStartStop covers the previously-untested recurring
// schedule path: Add registers a cron entry, Start begins firing it, and
// the enqueued job becomes visible on the queue. Stop halts further firing
// and returns a context that closes once the scheduler has drained.
func TestSchedulerAddStartStop(t *testing.T) {
	q := NewMemJobQueue()
	runner := &fakeRunner{}
	s := New(q, runner)

	spec := ScheduleSpec{
		Type:     "interval",
		Interval: 10 * time.Millisecond,
		Job:      Job{ID: "recurring", Type: "poll"},
	}
	if _, err := s.Add(spec); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}

	s.Start()

	ctx := context.Background()
	deadline := time.Now().Add(2 * time.Second)
	var claimed bool
	for time.Now().Before(deadline) {
		if _, ok, err := q.Claim(ctx, "node-1", time.Minute); err == nil && ok {
			claimed = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !claimed {
		t.Fatal("scheduled job was never enqueued after Start")
	}

	stopCtx := s.Stop()
	select {
	case <-stopCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Stop's context did not close in time")
	}
}

// TestSchedulerAddInvalidSpec ensures Add propagates a schedule expression
// validation error instead of registering a broken cron entry.
func TestSchedulerAddInvalidSpec(t *testing.T) {
	q := NewMemJobQueue()
	s := New(q, &fakeRunner{})

	if _, err := s.Add(ScheduleSpec{Type: "bogus"}); err == nil {
		t.Error("expected error for invalid schedule spec")
	}
}
