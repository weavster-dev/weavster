package scheduler

import "time"

// Lease is a durable job-claim lease with a heartbeat (gap #4 MVP).
type Lease struct {
	NodeID string
	Until  time.Time
}

// NewLease returns a lease owned by nodeID expiring after duration.
func NewLease(nodeID string, duration time.Duration) Lease {
	return Lease{NodeID: nodeID, Until: time.Now().Add(duration)}
}

// Expired reports whether the lease has lapsed as of now.
func (l Lease) Expired(now time.Time) bool {
	return !l.Until.IsZero() && l.Until.Before(now)
}
