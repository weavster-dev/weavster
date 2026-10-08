package main

import (
	"fmt"
	"time"
	_ "time/tzdata" // CRON_TZ works without the system's zone files (static, distroless builds)

	"github.com/robfig/cron/v3"

	"github.com/weavster-dev/weavster/internal/gateway"
)

// pollClock decides when polling sources (file, database) poll (#107
// D-77): last is each flow's last poll, again a time a flow polls again
// before its interval or schedule says so (a backlog, a file still being
// written, a failed scheduled poll). Callers serialize access.
type pollClock struct {
	last  map[string]time.Time
	again map[string]time.Time
}

func newPollClock() pollClock {
	return pollClock{last: map[string]time.Time{}, again: map[string]time.Time{}}
}

// due reports whether flow id's source polls at now, and records the poll.
// A retry time that has come is due first. Without a schedule a source is
// due when its interval has passed since its last poll (the first poll at
// once); with one, at each scheduled time after its last poll, the first
// being the first scheduled time after the source was first seen (so a
// time the server was down for is not made up). An invalid schedule is
// never due and returns an error.
func (c pollClock) due(src *gateway.FlowSource, id string, now time.Time, def time.Duration) (bool, error) {
	if at, ok := c.again[id]; ok && !now.Before(at) {
		delete(c.again, id)
		c.last[id] = now
		return true, nil
	}
	prev, seen := c.last[id]
	if src.Schedule != "" {
		sched, err := cron.ParseStandard(src.Schedule)
		if err != nil {
			return false, fmt.Errorf("source.schedule %q is not a cron expression", src.Schedule)
		}
		if !seen {
			c.last[id] = now
			return false, nil
		}
		// A zero Next is a time that never comes (February 30).
		if next := sched.Next(prev); next.IsZero() || next.After(now) {
			return false, nil
		}
		c.last[id] = now
		return true, nil
	}
	interval := def
	if src.PollIntervalMs > 0 {
		interval = time.Duration(src.PollIntervalMs) * time.Millisecond
	}
	if seen && now.Sub(prev) < interval {
		return false, nil
	}
	c.last[id] = now
	return true, nil
}

// retry makes flow id poll again at at, before its next regular time.
func (c pollClock) retry(id string, at time.Time) {
	if prev, ok := c.again[id]; !ok || at.Before(prev) {
		c.again[id] = at
	}
}

// forget drops flow id's times (its source stopped or went away).
func (c pollClock) forget(id string) {
	delete(c.last, id)
	delete(c.again, id)
}
