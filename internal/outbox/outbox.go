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
// idempotency key for this (message, destination), stable across retries.
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
	// ErrorCode gives a failure's protocol-specific code for the attempt
	// record (default: the code of an error with a Code() string method).
	ErrorCode func(error) string
}

// Outbox persists intent and result transactionally in the Store before
// acknowledging to the source (gap #5).
type Outbox struct {
	store   state.Store
	deliver DeliverFunc
	opts    Options
}

// codeOf is the code of an error with a Code() string method, or "".
func codeOf(err error) string {
	var c interface{ Code() string }
	if errors.As(err, &c) {
		return c.Code()
	}
	return ""
}

// New returns an outbox with sane defaults applied.
func New(store state.Store, deliver DeliverFunc, opts Options) *Outbox {
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 5
	}
	if opts.ErrorCode == nil {
		opts.ErrorCode = codeOf
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

// SetTransformed stores out as the message's transformed content (transform
// -> persist result, gap #5), its content type (unchanged when empty), and
// metadata (an empty value removes the key), in one write, so what the transform decided (such as excluded
// destinations) is never stored without its output or the other way round.
// It returns the stored message.
func (o *Outbox) SetTransformed(ctx context.Context, id string, out []byte, contentType string, metadata map[string]string) (state.Message, error) {
	m, err := o.store.Get(ctx, id)
	if err != nil {
		return state.Message{}, err
	}
	for k, v := range metadata {
		switch {
		case v == "":
			delete(m.Metadata, k)
		case m.Metadata == nil:
			m.Metadata = map[string]string{k: v}
		default:
			m.Metadata[k] = v
		}
	}
	m.Transformed = out
	if contentType != "" {
		m.ContentType = contentType
	}
	m.Status = state.StatusTransformed
	return m, o.store.Put(ctx, m)
}

// Deliver sends the message to one destination and records the outcome. It
// re-enters the same message id after a crash, and every attempt carries the
// same (message_id, destination) idempotency key, so the sink can drop
// duplicates (D-10).
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
			cur.LastError, cur.LastCode, cur.LastAttemptAt = "", "", time.Now()
			m.Attempts[dest] = cur
			return o.store.Put(ctx, m)
		}
	}

	attempt := cur.Attempts + 1
	key := IdempotencyKey(m.ID, dest)

	err = o.deliver(ctx, m, dest, key)
	cur.LastAttemptAt = time.Now()
	if err == nil {
		cur.Attempts = attempt
		cur.LastError, cur.LastCode = "", ""
		cur.NextAttemptAt = time.Time{}
		m.Attempts[dest] = cur
		// The message status is left to the caller: other destinations may
		// still be pending, so one success does not make the message sent.
		return o.store.Put(ctx, m)
	} else {
		cur.Attempts = attempt
		cur.LastCode = o.opts.ErrorCode(err)
		if errors.Is(err, ErrAmbiguous) {
			cur.LastError = ErrAmbiguous.Error()
		} else {
			cur.LastError = err.Error()
		}
		// The destination is exhausted at MaxAttempts (no next attempt). The
		// message stays queued: the caller decides dead-lettering once it has
		// considered every destination, so a crash here never strands work.
		if cur.Attempts >= o.opts.MaxAttempts {
			cur.NextAttemptAt = time.Time{}
		} else {
			cur.NextAttemptAt = time.Now().Add(o.Backoff(cur.Attempts))
		}
		m.Status = state.StatusQueued
		m.Attempts[dest] = cur
		return o.store.Put(ctx, m)
	}
}
