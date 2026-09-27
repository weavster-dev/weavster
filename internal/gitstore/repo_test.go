package gitstore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"
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
		rev, want string
		err       error
	}{
		{"HEAD", "flows/a.yaml", nil},
		{"HEAD~1", "flows/a.yaml,flows/b.yaml", nil},
		{"nope", "", ErrNotFound},
	} {
		files, err := s.FilesAt(tt.rev)
		if strings.Join(files, ",") != tt.want || !errors.Is(err, tt.err) {
			t.Errorf("FilesAt(%s) = %v %v", tt.rev, files, err)
		}
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

func TestRevisionsLimitAndUnstage(t *testing.T) {
	s, err := OpenOrInit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	staged := func() []string {
		t.Helper()
		st, err := s.wt.Status()
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for f, fs := range st {
			if fs.Staging != git.Unmodified && fs.Staging != git.Untracked {
				out = append(out, f)
			}
		}
		return out
	}
	// Before the first commit Unstage empties the index.
	if err := s.WriteFile("a.yaml", []byte("0")); err != nil {
		t.Fatal(err)
	}
	if err := s.wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.Unstage(); err != nil || len(staged()) != 0 {
		t.Errorf("staged after unstage: %v %v", staged(), err)
	}
	for i := 1; i <= 3; i++ {
		if err := s.WriteFile("a.yaml", []byte{byte('0' + i)}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Commit("c"+string(rune('0'+i)), Author{Name: "t"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tt := range []struct {
		path  string
		limit int
		want  string
	}{
		{"", 0, "c3,c2,c1"},
		{"", 2, "c3,c2"},
		{"a.yaml", 1, "c3"},
		{"other.yaml", 5, ""},
	} {
		revs, err := s.Revisions(tt.path, tt.limit)
		var got []string
		for _, r := range revs {
			got = append(got, r.Message)
		}
		if err != nil || strings.Join(got, ",") != tt.want {
			t.Errorf("%q/%d = %v %v, want %s", tt.path, tt.limit, got, err, tt.want)
		}
	}
	// After a commit Unstage resets the index; files keep their content.
	for f, v := range map[string]string{"a.yaml": "changed", "new.yaml": "new"} {
		if err := s.WriteFile(f, []byte(v)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.Unstage(); err != nil || len(staged()) != 0 {
		t.Errorf("staged after unstage: %v %v", staged(), err)
	}
	if a, _ := s.ReadFile("a.yaml"); string(a) != "changed" {
		t.Errorf("a.yaml = %q", a)
	}
	if n, _ := s.ReadFile("new.yaml"); string(n) != "new" {
		t.Errorf("new.yaml = %q", n)
	}
}
