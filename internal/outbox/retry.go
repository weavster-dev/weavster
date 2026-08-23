package outbox

import (
	"errors"
	"time"
)

// ErrAmbiguous signals a delivery whose outcome is unknown: the bytes may or
// may not have reached the destination (gap #5).
var ErrAmbiguous = errors.New("outbox: ambiguous delivery outcome")

// Backoff returns the delay before retry attempt n (1-based), exponential with
// a one-minute cap. Bounded by MaxAttempts, so retries never loop silently.
func (o *Outbox) Backoff(attempt int) time.Duration {
	d := o.opts.BackoffBase
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= time.Minute {
			return time.Minute
		}
	}
	return d
}
