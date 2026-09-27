package gitstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"
)

func TestDiffWorkingChangesRestore(t *testing.T) {
	ctx := context.Background()
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

	changes, patch, truncated, err := s.Diff(ctx, first, second)
	if err != nil || truncated || fmt.Sprint(changes) != "[{a.yaml modified} {b.yaml deleted} {c.yaml added}]" ||
		!strings.Contains(patch, "-a1\n+a2") {
		t.Errorf("diff = %v %q %v", changes, patch, err)
	}
	for _, tt := range [][2]string{{"nope", "HEAD"}, {"HEAD", "nope"}} {
		if _, _, _, err := s.Diff(ctx, tt[0], tt[1]); !errors.Is(err, ErrNotFound) {
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
	if _, _, err := s.RestoreTo(ctx, first, "", "r", author); !errors.Is(err, ErrUncommitted) {
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
		head, changed, err := s.RestoreTo(ctx, tt.rev, tt.path, "restore "+tt.name, author)
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
	ctx := context.Background()
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
	if _, _, err := s.RestoreTo(ctx, "HEAD~1", "", "restore", Author{Name: "t"}); err == nil {
		t.Fatal("restore over a read-only file succeeded")
	}
	if now, _, _ := s.Head(); now != head {
		t.Errorf("head moved to %s", now)
	}
}

// TestDiffAndRestoreEdges: a move is a deletion and an addition; a whole
// restore leaves ignored files alone; a staged-then-edited new file is
// added.
func TestDiffAndRestoreEdges(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := OpenOrInit(dir)
	if err != nil {
		t.Fatal(err)
	}
	commit := func(write map[string]string, remove ...string) string {
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
		h, err := s.Commit("c", Author{Name: "t"})
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	first := commit(map[string]string{".gitignore": "secret.txt\n", "flows/old.yaml": "same"})
	second := commit(map[string]string{"flows/new.yaml": "same"}, "flows/old.yaml")
	if files, _, _, err := s.Diff(ctx, first, second); err != nil || fmt.Sprint(files) != "[{flows/new.yaml added} {flows/old.yaml deleted}]" {
		t.Errorf("move = %v %v", files, err)
	}

	// An ignored file survives a whole-repository restore.
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := s.RestoreTo(ctx, first, "", "back", Author{Name: "t"}); err != nil || fmt.Sprint(changed) != "[flows/new.yaml flows/old.yaml]" {
		t.Errorf("restore = %v %v", changed, err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "secret.txt")); err != nil || string(b) != "keep" {
		t.Errorf("ignored file = %q %v", b, err)
	}

	// A new file staged and then edited is still added relative to HEAD.
	if err := s.WriteFile("n.yaml", []byte("1")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.wt.Add("n.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteFile("n.yaml", []byte("2")); err != nil {
		t.Fatal(err)
	}
	if w, err := s.WorkingChanges(); err != nil || fmt.Sprint(w) != "[{n.yaml added}]" {
		t.Errorf("working changes = %v %v", w, err)
	}
}

// TestDiffTruncatesLargePatches: a patch over MaxPatchBytes is cut and
// reported as truncated.
func TestDiffTruncatesLargePatches(t *testing.T) {
	s, err := OpenOrInit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var revs []string
	for _, c := range []string{"small\n", strings.Repeat("large line of text\n", MaxPatchBytes/19+10)} {
		if err := s.WriteFile("big.txt", []byte(c)); err != nil {
			t.Fatal(err)
		}
		h, err := s.Commit("c", Author{Name: "t"})
		if err != nil {
			t.Fatal(err)
		}
		revs = append(revs, h)
	}
	_, patch, truncated, err := s.Diff(context.Background(), revs[0], revs[1])
	if err != nil || !truncated || len(patch) != MaxPatchBytes {
		t.Errorf("truncated = %v, %d bytes, %v", truncated, len(patch), err)
	}
}

// TestRestoreUnknown: an unknown revision, or a repository without a
// commit, has nothing to restore.
func TestRestoreUnknown(t *testing.T) {
	ctx := context.Background()
	s, err := OpenOrInit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RestoreTo(ctx, "HEAD", "", "r", Author{Name: "t"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("no commit = %v", err)
	}
	if err := s.WriteFile("a.yaml", []byte("a")); err != nil {
		t.Fatal(err)
	}
	// Before the first commit every file is added.
	if w, err := s.WorkingChanges(); err != nil || fmt.Sprint(w) != "[{a.yaml added}]" {
		t.Errorf("working changes without a commit = %v %v", w, err)
	}
	if _, err := s.Commit("c", Author{Name: "t"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RestoreTo(ctx, "nope", "", "r", Author{Name: "t"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown revision = %v", err)
	}
}

// TestDiffRestoreCancelled: a cancelled request stops comparing trees.
func TestDiffRestoreCancelled(t *testing.T) {
	s, err := OpenOrInit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var revs []string
	for _, c := range []string{"1", "2"} {
		if err := s.WriteFile("a.yaml", []byte(c)); err != nil {
			t.Fatal(err)
		}
		h, err := s.Commit(c, Author{Name: "t"})
		if err != nil {
			t.Fatal(err)
		}
		revs = append(revs, h)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// go-git reports its own "operation canceled" error.
	if _, _, _, err := s.Diff(ctx, revs[0], revs[1]); err == nil {
		t.Error("cancelled diff succeeded")
	}
	if _, _, err := s.RestoreTo(ctx, revs[0], "", "r", Author{Name: "t"}); err == nil {
		t.Error("cancelled restore succeeded")
	}
	if head, _, _ := s.Head(); head != revs[1] {
		t.Errorf("head moved to %s", head)
	}
}
