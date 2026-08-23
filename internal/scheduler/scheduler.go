package scheduler

import (
	"context"
	"time"

	"github.com/robfig/cron/v3"
)

// Runner executes a claimed job. It is the Executor port, defined in the
// consuming package (hexagonal).
type Runner interface {
	Run(ctx context.Context, job Job) error
}

// Scheduler claims and runs jobs, and schedules recurring work on
// interval/cron specs (spec §2.4.11-12).
type Scheduler struct {
	queue  JobQueue
	runner Runner
	cron   *cron.Cron
}

// New returns a scheduler over the given queue and runner.
func New(queue JobQueue, runner Runner) *Scheduler {
	return &Scheduler{queue: queue, runner: runner, cron: cron.New()}
}

// Add registers a recurring schedule that enqueues the job when it fires.
func (s *Scheduler) Add(spec ScheduleSpec) (cron.EntryID, error) {
	expr, err := spec.Expr()
	if err != nil {
		return 0, err
	}
	return s.cron.AddFunc(expr, func() {
		_ = s.queue.Enqueue(context.Background(), spec.Job)
	})
}

// Start begins firing registered schedules.
func (s *Scheduler) Start() { s.cron.Start() }

// Stop halts firing and returns a context that closes when stopped.
func (s *Scheduler) Stop() context.Context { return s.cron.Stop() }

// RunDue claims and runs all currently-due jobs (deterministic path used by
// tests and the startup drain).
func (s *Scheduler) RunDue(ctx context.Context, nodeID string, lease time.Duration) (int, error) {
	ran := 0
	for {
		job, ok, err := s.queue.Claim(ctx, nodeID, lease)
		if err != nil {
			return ran, err
		}
		if !ok {
			return ran, nil
		}
		if err := s.runner.Run(ctx, job); err != nil {
			_ = s.queue.Requeue(ctx, job.ID, nodeID, err.Error())
			return ran, nil // requeued; re-attempted on the next cycle
		}
		_ = s.queue.Complete(ctx, job.ID, nodeID)
		ran++
	}
}
