// Package outbox implements the transactional outbox with idempotency keys,
// bounded retries, and dead-letter handling.
package outbox

import (
	"context"
	"errors"
	"time"

	"github.com/weavster-dev/weavster/internal/state"
)

// DeliverFunc attempts delivery of a message to one destination, receiving the
// deterministic idempotency key for this (message, destination, attempt).
// A nil return means delivered. Return ErrAmbiguous when the outcome is
// unknown (e.g. a timeout after the bytes may have been sent).
type DeliverFunc func(ctx context.Context, m state.Message, dest, idempotencyKey string) error

// StatusFunc checks whether an ambiguous delivery actually succeeded, so the
// outbox can check status rather than blindly re-send (gap #5).
type StatusFunc func(ctx context.Context, m state.Message, dest string) (bool, error)

// Options configures the outbox.
type Options struct {
	MaxAttempts int
	BackoffBase time.Duration
	CheckStatus StatusFunc
}

// Outbox persists intent and result transactionally in the Store before
// acknowledging to the source (gap #5).
type Outbox struct {
	store   state.Store
	deliver DeliverFunc
	opts    Options
}

// New returns an outbox with sane defaults applied.
func New(store state.Store, deliver DeliverFunc, opts Options) *Outbox {
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 5
	}
	if opts.BackoffBase <= 0 {
		opts.BackoffBase = time.Second
	}
	return &Outbox{store: store, deliver: deliver, opts: opts}
}

// Receive persists an incoming message (receive -> persist, gap #5).
func (o *Outbox) Receive(ctx context.Context, m state.Message) error {
	m.Status = state.StatusReceived
	return o.store.Put(ctx, m)
}

// Transform applies fn to the raw content and persists the result
// (transform -> persist result, gap #5).
func (o *Outbox) Transform(ctx context.Context, id string, fn func([]byte) ([]byte, error)) error {
	m, err := o.store.Get(ctx, id)
	if err != nil {
		return err
	}
	out, err := fn(m.Raw)
	if err != nil {
		return err
	}
	m.Transformed = out
	m.Status = state.StatusTransformed
	return o.store.Put(ctx, m)
}

// Deliver sends the message to one destination and records the outcome. It
// re-enters the same message id after a crash, so a retry never duplicates a
// message with the same (message_id, destination, attempt) idempotency key.
func (o *Outbox) Deliver(ctx context.Context, id, dest string) error {
	m, err := o.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if m.Attempts == nil {
		m.Attempts = make(map[string]state.DestinationAttempt)
	}
	cur := m.Attempts[dest]

	// Ambiguous prior outcome: check status first, do not blindly re-send.
	if cur.LastError == ErrAmbiguous.Error() && o.opts.CheckStatus != nil {
		delivered, err := o.opts.CheckStatus(ctx, m, dest)
		if err != nil {
			return err
		}
		if delivered {
			cur.Attempts++
			cur.LastError = ""
			m.Attempts[dest] = cur
			m.Status = state.StatusSent
			return o.store.Put(ctx, m)
		}
	}

	attempt := cur.Attempts + 1
	key := IdempotencyKey(m.ID, dest, attempt)

	if err := o.deliver(ctx, m, dest, key); err == nil {
		cur.Attempts = attempt
		cur.LastError = ""
		m.Attempts[dest] = cur
		m.Status = state.StatusSent
		return o.store.Put(ctx, m)
	} else {
		cur.Attempts = attempt
		if errors.Is(err, ErrAmbiguous) {
			cur.LastError = ErrAmbiguous.Error()
		} else {
			cur.LastError = err.Error()
		}
		m.Attempts[dest] = cur
		if cur.Attempts >= o.opts.MaxAttempts {
			m.Status = state.StatusErrored // dead-letter
		} else {
			m.Status = state.StatusQueued
		}
		return o.store.Put(ctx, m)
	}
}
