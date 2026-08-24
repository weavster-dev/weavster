package executor

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/tetratelabs/wazero/sys"
)

// TestIsDeadline exercises every branch of isDeadline directly: bare and
// wrapped context errors, wazero sys.ExitError deadline/cancel codes, an
// unrelated ExitError exit code, and a plain unrelated error. isDeadline
// classifies which Limits field (fuel vs. timeout) aborted a run, so a
// misclassification here would surface the wrong LimitType to callers.
func TestIsDeadline(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"context.DeadlineExceeded", context.DeadlineExceeded, true},
		{"context.Canceled", context.Canceled, true},
		{
			"wrapped context.DeadlineExceeded",
			fmt.Errorf("run failed: %w", context.DeadlineExceeded),
			true,
		},
		{
			"wrapped context.Canceled",
			fmt.Errorf("run failed: %w", context.Canceled),
			true,
		},
		{
			"sys.ExitError deadline exceeded",
			sys.NewExitError(sys.ExitCodeDeadlineExceeded),
			true,
		},
		{
			"sys.ExitError context canceled",
			sys.NewExitError(sys.ExitCodeContextCanceled),
			true,
		},
		{
			"sys.ExitError unrelated exit code",
			sys.NewExitError(1),
			false,
		},
		{
			"sys.ExitError success code",
			sys.NewExitError(0),
			false,
		},
		{
			"unrelated error",
			errors.New("boom"),
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDeadline(tt.err); got != tt.want {
				t.Errorf("isDeadline(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestWithDeadlineNoLimits verifies that when neither Fuel nor Timeout is
// set, withDeadline is a no-op: it returns the original context unchanged,
// a callable no-op cancel func, and an empty LimitType.
func TestWithDeadlineNoLimits(t *testing.T) {
	ctx := context.Background()
	l := Limits{}
	gotCtx, cancel, limit := l.withDeadline(ctx)
	defer cancel()

	if gotCtx != ctx {
		t.Errorf("withDeadline returned a different context when no limits set")
	}
	if limit != "" {
		t.Errorf("limit = %q, want empty", limit)
	}
	if _, ok := gotCtx.Deadline(); ok {
		t.Errorf("expected no deadline on returned context")
	}
}
