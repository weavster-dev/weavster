package executor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tetratelabs/wazero/sys"
)

// Limits are per-instantiation resource limits (arch §4.3).
type Limits struct {
	// Fuel is a CPU-time budget. wazero exposes no instruction-count API, so
	// fuel is enforced as a wall-clock deadline (documented deviation from
	// arch §4.3; see CHANGELOG).
	Fuel time.Duration
	// MemoryPages caps guest memory in 64KiB pages (0 = runtime default).
	MemoryPages uint32
	// Timeout is the wall-clock deadline (0 = none).
	Timeout time.Duration
}

// LimitType identifies which limit aborted a run (arch §4.3).
type LimitType string

const (
	LimitFuel   LimitType = "fuel"
	LimitMemory LimitType = "memory"
	LimitTime   LimitType = "time"
)

// LimitError is the structured error carrying module name + version + input
// hash + limit type (arch §4.3, gap #3 MVP).
type LimitError struct {
	Module    string
	Version   string
	InputHash string
	Limit     LimitType
	Err       error
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("executor: %s limit exceeded for module %s@%s (input %s): %v",
		e.Limit, e.Module, e.Version, e.InputHash, e.Err)
}

func (e *LimitError) Unwrap() error { return e.Err }

// withDeadline applies fuel/timeout as a context deadline, returning the
// derived context and the limit type that will fire first.
func (l Limits) withDeadline(ctx context.Context) (context.Context, context.CancelFunc, LimitType) {
	var deadline time.Time
	limit := LimitTime
	if l.Fuel > 0 {
		deadline = time.Now().Add(l.Fuel)
		limit = LimitFuel
	}
	if l.Timeout > 0 {
		d := time.Now().Add(l.Timeout)
		if deadline.IsZero() || d.Before(deadline) {
			deadline = d
			limit = LimitTime
		}
	}
	if deadline.IsZero() {
		return ctx, func() {}, ""
	}
	c, cancel := context.WithDeadline(ctx, deadline)
	return c, cancel, limit
}

// isDeadline reports whether err is a context deadline/cancel, including
// wazero's sys.ExitError raised when WithCloseOnContextDone terminates a run.
func isDeadline(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var exitErr *sys.ExitError
	if errors.As(err, &exitErr) {
		c := exitErr.ExitCode()
		return c == sys.ExitCodeDeadlineExceeded || c == sys.ExitCodeContextCanceled
	}
	return false
}
