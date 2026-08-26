package executor

import (
	"context"
	"errors"
	"testing"
)

// TestIsMemoryError exercises every branch of isMemoryError directly: nil
// error, errors whose message contains "memory" or "exceeds" (the substrings
// wazero/host code uses to report out-of-memory conditions), and an
// unrelated error. isMemoryError gates whether wrapRunErr reports a failure
// as LimitMemory, so a misclassification here would hide the true cause of
// an out-of-memory guest failure from callers.
func TestIsMemoryError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"contains memory", errors.New("out of memory"), true},
		{"contains exceeds", errors.New("allocation exceeds limit"), true},
		{"contains both", errors.New("memory allocation exceeds cap"), true},
		{"unrelated error", errors.New("boom"), false},
		{"empty message", errors.New(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isMemoryError(tt.err); got != tt.want {
				t.Errorf("isMemoryError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestWrapRunErrMemoryClassification confirms wrapRunErr surfaces a
// LimitError with Limit=LimitMemory only when the request opted into a
// memory limit (Limits.MemoryPages > 0) and the underlying error matches
// isMemoryError, and otherwise falls back to a plain wrapped error.
func TestWrapRunErrMemoryClassification(t *testing.T) {
	baseReq := Request{ModuleName: "mod", Version: "v1"}

	t.Run("memory limit configured and error matches", func(t *testing.T) {
		req := baseReq
		req.Limits.MemoryPages = 4
		err := wrapRunErr(context.Background(), req, "", errors.New("out of memory"))
		var limitErr *LimitError
		if !errors.As(err, &limitErr) {
			t.Fatalf("wrapRunErr() = %v, want *LimitError", err)
		}
		if limitErr.Limit != LimitMemory {
			t.Errorf("Limit = %v, want %v", limitErr.Limit, LimitMemory)
		}
	})

	t.Run("no memory limit configured", func(t *testing.T) {
		req := baseReq
		err := wrapRunErr(context.Background(), req, "", errors.New("out of memory"))
		var limitErr *LimitError
		if errors.As(err, &limitErr) {
			t.Errorf("wrapRunErr() = %v, want plain error (MemoryPages=0)", err)
		}
	})

	t.Run("memory limit configured but unrelated error", func(t *testing.T) {
		req := baseReq
		req.Limits.MemoryPages = 4
		err := wrapRunErr(context.Background(), req, "", errors.New("boom"))
		var limitErr *LimitError
		if errors.As(err, &limitErr) {
			t.Errorf("wrapRunErr() = %v, want plain error (unrelated message)", err)
		}
	})
}
