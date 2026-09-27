package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMoveFile(t *testing.T) {
	src, dest := t.TempDir(), filepath.Join(t.TempDir(), "done")
	write := func(name, body string) string {
		p := filepath.Join(src, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// Into a directory that does not exist yet.
	if err := moveFile(write("a.json", "1"), dest, "a.json", "m1"); err != nil {
		t.Fatal(err)
	}
	// A name already there gets the suffix, or a timestamp without one.
	if err := moveFile(write("a.json", "2"), dest, "a.json", "m2"); err != nil {
		t.Fatal(err)
	}
	if err := moveFile(write("a.json", "3"), dest, "a.json", ""); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dest)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 3 || names[0] != "a.json" || !strings.HasPrefix(names[1], "a.json.") || names[2] != "a.json.m2" {
		t.Errorf("moved = %v", names)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "a.json.m2")); string(b) != "2" {
		t.Errorf("a.json.m2 = %q", b)
	}
	if left, _ := os.ReadDir(src); len(left) != 0 {
		t.Errorf("source not emptied: %v", left)
	}
	// Errors: the source is missing, or the destination cannot be created.
	if err := moveFile(filepath.Join(src, "none"), dest, "none", ""); err == nil {
		t.Error("moving a missing file succeeded")
	}
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := moveFile(write("b.json", "x"), filepath.Join(blocker, "sub"), "b.json", ""); err == nil {
		t.Error("moving under a file succeeded")
	}
}
