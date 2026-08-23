package scheduler

import "context"

// Reconcile re-queues jobs whose lease expired so a stale node cannot
// double-claim them (startup reconciler; gap #4 MVP, spec §2.5.16).
func (s *Scheduler) Reconcile(ctx context.Context, nodeID string) (int, error) {
	return s.queue.Reconcile(ctx, nodeID)
}
