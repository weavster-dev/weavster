package auth

import "time"

// LockoutPolicy is the account-lockout policy (spec §2.13.42, §4.4). A zero
// RetryLimit disables lockout.
type LockoutPolicy struct {
	RetryLimit    int
	LockoutPeriod int // seconds
}

func (p LockoutPolicy) isLocked(until time.Time) bool {
	return p.RetryLimit > 0 && time.Now().Before(until)
}

func (p LockoutPolicy) expired(until, now time.Time) bool {
	return !until.IsZero() && !now.Before(until)
}
