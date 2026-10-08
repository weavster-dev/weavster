package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

var errResultOutput = errors.New("result output unavailable")

type failingResultWriter struct{}

func (failingResultWriter) Write([]byte) (int, error) {
	return 0, errResultOutput
}

func TestRunTestReportsResultOutputFailures(t *testing.T) {
	for _, format := range []string{"junit", "json"} {
		t.Run(format, func(t *testing.T) {
			var stderr bytes.Buffer
			code := runTest(
				[]string{"--format", format},
				failingResultWriter{},
				&stderr,
			)

			if code != 2 {
				t.Errorf("runTest exit code = %d, want 2", code)
			}
			if !strings.Contains(stderr.String(), errResultOutput.Error()) {
				t.Errorf("stderr = %q, want output failure", stderr.String())
			}
		})
	}
}
