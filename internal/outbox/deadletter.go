package outbox

import (
	"context"

	"github.com/weavster-dev/weavster/internal/state"
)

// DeadLetter returns messages that exhausted their retry budget
// (the deadletter admin surface, gap #5).
func (o *Outbox) DeadLetter(ctx context.Context) ([]state.Message, error) {
	return o.store.Search(ctx, state.Query{Status: state.StatusErrored, Limit: 1000})
}

// Requeue returns a dead-lettered message to the queue, resetting its attempt
// counters so it can be retried.
func (o *Outbox) Requeue(ctx context.Context, id string) error {
	m, err := o.store.Get(ctx, id)
	if err != nil {
		return err
	}
	m.Status = state.StatusQueued
	for d := range m.Attempts {
		m.Attempts[d] = state.DestinationAttempt{}
	}
	return o.store.Put(ctx, m)
}
