package scheduler

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite" // SQLite driver for local-DX queue tests
)

type fakeRunner struct {
	ran []string
	err error
}

func (r *fakeRunner) Run(_ context.Context, job Job) error {
	r.ran = append(r.ran, job.ID)
	return r.err
}

func newSQLiteQueue(t *testing.T) *SQLJobQueue {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	q, err := NewSQLJobQueue(db, "sqlite")
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func testQueue(t *testing.T) map[string]JobQueue {
	t.Helper()
	return map[string]JobQueue{
		"memory": NewMemJobQueue(),
		"sqlite": newSQLiteQueue(t),
	}
}

func TestQueueClaimComplete(t *testing.T) {
	for name, q := range testQueue(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			if err := q.Enqueue(ctx, Job{ID: "j1", Type: "poll"}); err != nil {
				t.Fatal(err)
			}
			job, ok, err := q.Claim(ctx, "node-a", time.Minute)
			if err != nil || !ok || job.ID != "j1" {
				t.Fatalf("claim = %+v, %v, %v", job, ok, err)
			}
			// A second node cannot claim while the lease is valid.
			if _, ok, _ := q.Claim(ctx, "node-b", time.Minute); ok {
				t.Error("second node claimed a leased job")
			}
			if err := q.Complete(ctx, "j1", "node-a"); err != nil {
				t.Fatal(err)
			}
			if _, ok, _ := q.Claim(ctx, "node-a", time.Minute); ok {
				t.Error("completed job still claimable")
			}
		})
	}
}

func TestQueueRequeueAndReconcile(t *testing.T) {
	for name, q := range testQueue(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			_ = q.Enqueue(ctx, Job{ID: "j1", Type: "poll"})

			job, ok, _ := q.Claim(ctx, "node-a", time.Minute)
			if !ok {
				t.Fatal("claim failed")
			}
			_ = job

			// Requeue keeps the job (retry).
			if err := q.Requeue(ctx, "j1", "node-a", "boom"); err != nil {
				t.Fatal(err)
			}
			if _, ok, _ := q.Claim(ctx, "node-a", time.Minute); !ok {
				t.Error("requeued job not claimable")
			}
			_ = q.Requeue(ctx, "j1", "node-a", "again")

			// Simulate a crash: job stuck in running with an expired lease.
			_, ok, _ = q.Claim(ctx, "node-a", -time.Second) // lease already expired
			if !ok {
				t.Fatal("claim with expired lease failed")
			}
			n, err := q.Reconcile(ctx, "node-b")
			if err != nil || n != 1 {
				t.Fatalf("reconcile = %d, %v", n, err)
			}
			if _, ok, _ := q.Claim(ctx, "node-b", time.Minute); !ok {
				t.Error("reconciled job not claimable by a new node")
			}
		})
	}
}

func TestSchedulerRunDue(t *testing.T) {
	q := NewMemJobQueue()
	runner := &fakeRunner{}
	s := New(q, runner)
	ctx := context.Background()

	_ = q.Enqueue(ctx, Job{ID: "a", Type: "poll"})
	_ = q.Enqueue(ctx, Job{ID: "b", Type: "poll"})

	ran, err := s.RunDue(ctx, "node-1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if ran != 2 || len(runner.ran) != 2 {
		t.Errorf("ran = %d, jobs = %v", ran, runner.ran)
	}
}

func TestSchedulerRunDueRequeuesOnError(t *testing.T) {
	q := NewMemJobQueue()
	runner := &fakeRunner{err: context.DeadlineExceeded}
	s := New(q, runner)
	ctx := context.Background()
	_ = q.Enqueue(ctx, Job{ID: "a", Type: "poll"})

	if _, err := s.RunDue(ctx, "node-1", time.Minute); err != nil {
		t.Fatal(err)
	}
	// The failed job is requeued and claimable.
	if _, ok, _ := q.Claim(ctx, "node-1", time.Minute); !ok {
		t.Error("failed job was not requeued")
	}
}

func TestScheduleSpecExpr(t *testing.T) {
	iv, err := (ScheduleSpec{Type: "interval", Interval: time.Hour}).Expr()
	if err != nil || iv != "@every 1h0m0s" {
		t.Errorf("interval expr = %q, %v", iv, err)
	}
	cronExpr, err := (ScheduleSpec{Type: "cron", Cron: "0 */5 * * *"}).Expr()
	if err != nil || cronExpr != "0 */5 * * *" {
		t.Errorf("cron expr = %q, %v", cronExpr, err)
	}
	if _, err := (ScheduleSpec{Type: "cron", Cron: "not-a-cron"}).Expr(); err == nil {
		t.Error("expected error for invalid cron")
	}
	if _, err := (ScheduleSpec{Type: "interval", Interval: 0}).Expr(); err == nil {
		t.Error("expected error for zero interval")
	}
}

func TestLeaseExpiry(t *testing.T) {
	now := time.Now()
	if !NewLease("n", -time.Second).Expired(now) {
		t.Error("negative-duration lease must be expired")
	}
	if NewLease("n", time.Minute).Expired(now) {
		t.Error("future lease must not be expired")
	}
}
