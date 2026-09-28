package main

import (
	"sync"
	"time"
	_ "time/tzdata" // CRON_TZ works without the system's zone files (static, distroless builds)

	"github.com/robfig/cron/v3"

	"github.com/weavster-dev/weavster/internal/gateway"
)

// schedules caches parsed source schedules by expression.
var schedules sync.Map // string -> cron.Schedule

// pollDue reports whether a polling source (file or database) is due at
// now, and records the poll in last (#107 D-77). Without a schedule it is
// due when its interval has passed since the last poll, the first poll at
// once; with one, at each scheduled time after the last poll, the first
// being the first scheduled time after the source was first seen.
func pollDue(src *gateway.FlowSource, last map[string]time.Time, id string, now time.Time, def time.Duration) bool {
	prev, seen := last[id]
	if src.Schedule != "" {
		sched, ok := parseSchedule(src.Schedule)
		if !ok {
			return false // refused with the definition; never due
		}
		if !seen {
			last[id] = now
			return false
		}
		if sched.Next(prev).After(now) {
			return false
		}
		last[id] = now
		return true
	}
	interval := def
	if src.PollIntervalMs > 0 {
		interval = time.Duration(src.PollIntervalMs) * time.Millisecond
	}
	if seen && now.Sub(prev) < interval {
		return false
	}
	last[id] = now
	return true
}

// parseSchedule parses a source schedule once.
func parseSchedule(expr string) (cron.Schedule, bool) {
	if s, ok := schedules.Load(expr); ok {
		return s.(cron.Schedule), true
	}
	s, err := cron.ParseStandard(expr)
	if err != nil {
		return nil, false
	}
	schedules.Store(expr, s)
	return s, true
}
