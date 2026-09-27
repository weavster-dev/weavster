package gitstore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenOrInitFilesHeadRevisions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "repo")
	s, err := OpenOrInit(dir)
	if err != nil {
		t.Fatal(err)
	}
	if hash, branch, err := s.Head(); err != nil || hash != "" || branch != "main" {
		t.Fatalf("empty head = %q %q %v", hash, branch, err)
	}
	author := Author{Name: "tester"}
	for _, f := range []string{"flows/a.yaml", "flows/b.yaml"} {
		if err := s.WriteFile(f, []byte(f)); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.Commit("two flows", author)
	if err != nil {
		t.Fatal(err)
	}
	// A deletion is committed too.
	if err := s.RemoveFile("flows/b.yaml"); err != nil {
		t.Fatal(err)
	}
	second, err := s.Commit("remove b", author)
	if err != nil {
		t.Fatal(err)
	}
	if changed, _ := s.WorkingTreeDiff(); len(changed) != 0 {
		t.Errorf("deletion left uncommitted: %v", changed)
	}

	// Reopening finds the same repository.
	s, err = OpenOrInit(dir)
	if err != nil {
		t.Fatal(err)
	}
	if hash, branch, err := s.Head(); err != nil || hash != second || branch != "main" {
		t.Errorf("head = %q %q %v, want %q main", hash, branch, err, second)
	}
	if files, err := s.Files(); err != nil || strings.Join(files, ",") != "flows/a.yaml" {
		t.Errorf("files = %v %v", files, err)
	}
	for _, tt := range []struct {
		path, rev, want string
		err             error
	}{
		{"flows/b.yaml", first, "flows/b.yaml", nil},
		{"flows/b.yaml", first[:7], "flows/b.yaml", nil},
		{"flows/b.yaml", "HEAD~1", "flows/b.yaml", nil},
		{"flows/a.yaml", "main", "flows/a.yaml", nil},
		{"flows/b.yaml", "HEAD", "", ErrNotFound},
		{"flows/a.yaml", "no-such-rev", "", ErrNotFound},
		{"flows/a.yaml", strings.Repeat("0", 40), "", ErrNotFound},
	} {
		got, err := s.ContentAtRevision(tt.path, tt.rev)
		if string(got) != tt.want || !errors.Is(err, tt.err) {
			t.Errorf("%s@%s = %q %v, want %q %v", tt.path, tt.rev, got, err, tt.want, tt.err)
		}
	}
}

func TestOpenOrInitErrors(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(file, "repo")} { // parent is a file
		if _, err := OpenOrInit(path); err == nil {
			t.Errorf("%s: no error", path)
		}
	}
}
