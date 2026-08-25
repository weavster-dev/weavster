package scheduler

import (
	"context"
	"testing"
	"time"
)

// TestQueueHeartbeat covers JobQueue.Heartbeat across both queue
// implementations: renewing the lease of the owning node, rejecting an
// unknown job, and rejecting a node that does not currently hold the claim.
func TestQueueHeartbeat(t *testing.T) {
	for name, q := range testQueue(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			if err := q.Heartbeat(ctx, "missing", "node-a", time.Minute); err == nil {
				t.Error("expected error heartbeating an unknown job")
			}

			if err := q.Enqueue(ctx, Job{ID: "j1", Type: "poll"}); err != nil {
				t.Fatal(err)
			}
			if _, ok, err := q.Claim(ctx, "node-a", time.Second); err != nil || !ok {
				t.Fatalf("claim = %v, %v", ok, err)
			}

			// A different node cannot renew a lease it does not own.
			if err := q.Heartbeat(ctx, "j1", "node-b", time.Minute); err == nil {
				t.Error("expected error heartbeating with a non-owning node")
			}

			// The owning node can renew the lease, extending it well past
			// the original (short) lease duration.
			if err := q.Heartbeat(ctx, "j1", "node-a", time.Minute); err != nil {
				t.Fatalf("heartbeat: %v", err)
			}

			// Reconcile should not touch a job whose lease was just renewed.
			n, err := q.Reconcile(ctx, "node-b")
			if err != nil {
				t.Fatalf("reconcile: %v", err)
			}
			if n != 0 {
				t.Errorf("reconcile requeued %d jobs, want 0 after heartbeat renewal", n)
			}
			if _, ok, _ := q.Claim(ctx, "node-b", time.Minute); ok {
				t.Error("job with a freshly renewed lease should not be claimable")
			}
		})
	}
}

// TestSchedulerReconcileAfterHeartbeatExpiry covers Scheduler.Reconcile,
// which delegates to the underlying queue's Reconcile to re-queue jobs whose
// lease expired (e.g. a node crashed mid-run), so a new node can claim and
// run them.
func TestSchedulerReconcileAfterHeartbeatExpiry(t *testing.T) {
	q := NewMemJobQueue()
	s := New(q, &fakeRunner{})
	ctx := context.Background()

	_ = q.Enqueue(ctx, Job{ID: "a", Type: "poll"})
	// Claim with an already-expired lease to simulate a crashed node.
	if _, ok, err := q.Claim(ctx, "node-1", -time.Second); err != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}

	n, err := s.Reconcile(ctx, "node-2")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if n != 1 {
		t.Errorf("reconcile = %d, want 1", n)
	}
	if _, ok, err := q.Claim(ctx, "node-2", time.Minute); err != nil || !ok {
		t.Error("expected reconciled job to be claimable by a new node")
	}
}
