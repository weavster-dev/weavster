package executor

import (
	"errors"
	"testing"
)

func TestIsMemoryError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"memory keyword", errors.New("out of memory"), true},
		{"exceeds keyword", errors.New("allocation exceeds limit"), true},
		{"unrelated error", errors.New("connection refused"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isMemoryError(tc.err); got != tc.want {
				t.Fatalf("isMemoryError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
