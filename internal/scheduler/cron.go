package scheduler

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// ScheduleSpec describes a recurring schedule (interval or cron) and the job
// it enqueues (spec §2.4.11-12).
type ScheduleSpec struct {
	Type     string // "interval" | "cron"
	Interval time.Duration
	Cron     string
	Job      Job
}

// Expr returns the cron expression string for the spec, validating it.
func (s ScheduleSpec) Expr() (string, error) {
	switch s.Type {
	case "interval":
		if s.Interval <= 0 {
			return "", fmt.Errorf("scheduler: interval must be positive")
		}
		return "@every " + s.Interval.String(), nil
	case "cron":
		if _, err := cron.ParseStandard(s.Cron); err != nil {
			return "", fmt.Errorf("scheduler: invalid cron %q: %w", s.Cron, err)
		}
		return s.Cron, nil
	default:
		return "", fmt.Errorf("scheduler: unknown schedule type %q", s.Type)
	}
}
