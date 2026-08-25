package executor

import (
	"errors"
	"testing"
)

// TestIsMemoryError covers the wazero error-message sniffing used to
// distinguish a memory-limit violation from other trap types. This
// determines whether a Transform failure is reported as a LimitError
// (LimitMemory) or a generic execution error, so callers can tell an
// intentional resource-limit rejection apart from a guest bug.
func TestIsMemoryError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"unrelated error", errors.New("some other trap"), false},
		{"mentions memory", errors.New("out of memory"), true},
		{"mentions exceeds", errors.New("allocation exceeds limit"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isMemoryError(tc.err); got != tc.want {
				t.Errorf("isMemoryError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
