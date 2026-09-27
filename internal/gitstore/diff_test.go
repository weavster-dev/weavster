package gitstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"
)

func TestDiffWorkingChangesRestore(t *testing.T) {
	s, err := OpenOrInit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	author := Author{Name: "t"}
	step := func(write map[string]string, remove ...string) string {
		t.Helper()
		for f, c := range write {
			if err := s.WriteFile(f, []byte(c)); err != nil {
				t.Fatal(err)
			}
		}
		for _, f := range remove {
			if err := s.RemoveFile(f); err != nil {
				t.Fatal(err)
			}
		}
		h, err := s.Commit("step", author)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	first := step(map[string]string{"a.yaml": "a1\n", "b.yaml": "b1\n"})
	second := step(map[string]string{"a.yaml": "a2\n", "c.yaml": "c1\n"}, "b.yaml")

	changes, patch, err := s.Diff(first, second)
	if err != nil || fmt.Sprint(changes) != "[{a.yaml modified} {b.yaml deleted} {c.yaml added}]" ||
		!strings.Contains(patch, "-a1\n+a2") {
		t.Errorf("diff = %v %q %v", changes, patch, err)
	}
	for _, tt := range [][2]string{{"nope", "HEAD"}, {"HEAD", "nope"}} {
		if _, _, err := s.Diff(tt[0], tt[1]); !errors.Is(err, ErrNotFound) {
			t.Errorf("diff %v = %v", tt, err)
		}
	}

	// Working-tree changes, and restore refuses to run over them.
	if err := s.WriteFile("a.yaml", []byte("edited")); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteFile("new.yaml", []byte("n")); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveFile("c.yaml"); err != nil {
		t.Fatal(err)
	}
	if w, err := s.WorkingChanges(); err != nil || fmt.Sprint(w) != "[{a.yaml modified} {c.yaml deleted} {new.yaml added}]" {
		t.Errorf("working changes = %v %v", w, err)
	}
	if _, _, err := s.RestoreTo(first, "", "r", author); !errors.Is(err, ErrUncommitted) {
		t.Errorf("restore over changes = %v", err)
	}
	if err := s.wt.Reset(&git.ResetOptions{Mode: git.HardReset}); err != nil { // discard the edits
		t.Fatal(err)
	}

	for _, tt := range []struct {
		name, rev, path string
		changed         string
		files           string
		err             error
	}{
		{"one file", first, "a.yaml", "[a.yaml]", "a.yaml=a1,c.yaml=c1", nil},
		{"unchanged file", first, "a.yaml", "[]", "a.yaml=a1,c.yaml=c1", nil},
		{"whole repository", first, "", "[b.yaml c.yaml]", "a.yaml=a1,b.yaml=b1", nil},
		{"file missing at revision", second, "b.yaml", "[]", "a.yaml=a1,b.yaml=b1", ErrNotFound},
		{"unknown revision", "nope", "", "[]", "a.yaml=a1,b.yaml=b1", ErrNotFound},
	} {
		head, changed, err := s.RestoreTo(tt.rev, tt.path, "restore "+tt.name, author)
		if !errors.Is(err, tt.err) || fmt.Sprint(changed) != tt.changed || (len(changed) > 0) != (head != "") {
			t.Errorf("%s: %s %v %v", tt.name, head, changed, err)
		}
		files, _ := s.Files()
		var got []string
		for _, f := range files {
			b, _ := s.ReadFile(f)
			got = append(got, f+"="+strings.TrimSpace(string(b)))
		}
		if strings.Join(got, ",") != tt.files {
			t.Errorf("%s: files = %v", tt.name, got)
		}
	}
	// Restores are new commits: the history is kept.
	if revs, _ := s.Log(); len(revs) != 4 || revs[0].Message != "restore whole repository" {
		t.Errorf("log = %+v", revs)
	}
}

// TestRestoreFailure: a restore that cannot write a file fails
// without committing anything.
func TestRestoreFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes read-only directories")
	}
	dir := t.TempDir()
	s, err := OpenOrInit(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"one", "two"} {
		if err := s.WriteFile("a.yaml", []byte(c)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Commit(c, Author{Name: "t"}); err != nil {
			t.Fatal(err)
		}
	}
	head, _, _ := s.Head()
	file := filepath.Join(dir, "a.yaml")
	if err := os.Chmod(file, 0o400); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(file, 0o600) }()
	if _, _, err := s.RestoreTo("HEAD~1", "", "restore", Author{Name: "t"}); err == nil {
		t.Fatal("restore over a read-only file succeeded")
	}
	if now, _, _ := s.Head(); now != head {
		t.Errorf("head moved to %s", now)
	}
}
