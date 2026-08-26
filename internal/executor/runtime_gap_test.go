package executor

import (
	"errors"
	"testing"
)

// TestIsMemoryError covers isMemoryError, which determines whether a wasm
// runtime error should be classified as a memory-limit LimitError in
// wrapRunErr. An incorrect classification here would misreport memory
// exhaustion as a generic execution failure (or vice versa), which affects
// operator-facing error messages and alerting.
func TestIsMemoryError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"memory keyword", errors.New("out of memory"), true},
		{"exceeds keyword", errors.New("allocation exceeds limit"), true},
		{"unrelated error", errors.New("division by zero"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isMemoryError(tc.err); got != tc.want {
				t.Errorf("isMemoryError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
