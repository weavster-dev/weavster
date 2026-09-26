package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunTransformErrors(t *testing.T) {
	tests := []struct {
		name    string
		codec   string
		content []byte
	}{
		{name: "unknown codec", codec: "no-such-codec", content: []byte("x")},
		{name: "malformed json", codec: "json", content: []byte("{not json")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := runTransform(tt.codec, tt.content); err == nil {
				t.Errorf("runTransform(%q) error = nil, want error", tt.codec)
			}
		})
	}
}

func TestRunTestUsageError(t *testing.T) {
	var out, errb bytes.Buffer
	if code := runTest([]string{"--bogus"}, &out, &errb); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}

func TestRunTestFilterSkipsFixtures(t *testing.T) {
	var out, errb bytes.Buffer
	if code := runTest([]string{"--format", "json", "--filter", "raw"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errb.String())
	}
	if !strings.Contains(out.String(), "identity/raw") {
		t.Errorf("output missing filtered fixture: %q", out.String())
	}
	if strings.Contains(out.String(), "identity/hl7") {
		t.Errorf("output contains fixture excluded by filter: %q", out.String())
	}
}

// TestRunTestOutputWriteError covers the exit-2 path when results cannot be
// written: --output points below a regular file, so MkdirAll fails.
func TestRunTestOutputWriteError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	for _, format := range []string{"junit", "json"} {
		t.Run(format, func(t *testing.T) {
			var out, errb bytes.Buffer
			code := runTest([]string{"--format", format, "--output", filepath.Join(file, "sub")}, &out, &errb)
			if code != 2 {
				t.Errorf("exit = %d, want 2", code)
			}
			if !strings.Contains(errb.String(), "Error:") {
				t.Errorf("stderr = %q, want error message", errb.String())
			}
		})
	}
}

func TestWriteJUnitResultsCountsFailures(t *testing.T) {
	var out bytes.Buffer
	results := []testResult{
		{Name: "ok", Passed: true},
		{Name: "bad", Passed: false, Failure: "boom"},
	}
	if err := writeJUnitResults("", &out, results); err != nil {
		t.Fatalf("writeJUnitResults: %v", err)
	}
	got := out.String()
	for _, want := range []string{`tests="2"`, `failures="1"`, `<failure>boom</failure>`} {
		if !strings.Contains(got, want) {
			t.Errorf("junit output missing %q: %q", want, got)
		}
	}
}
