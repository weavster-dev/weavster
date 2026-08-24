package gitstore

import (
	"path/filepath"
	"testing"

	git "github.com/go-git/go-git/v5"
)

func TestCommitHistoryContentRestore(t *testing.T) {
	s, err := NewMem()
	if err != nil {
		t.Fatal(err)
	}

	if err := s.WriteFile("flows/a.yaml", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	h1, err := s.Commit("add a", Author{Name: "tester", Email: "t@example.com"})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.WriteFile("flows/a.yaml", []byte("v2")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Commit("update a", Author{Name: "tester", Email: "t@example.com"}); err != nil {
		t.Fatal(err)
	}

	log, err := s.Log()
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 2 {
		t.Fatalf("log length = %d, want 2", len(log))
	}

	hist, err := s.History("flows/a.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 {
		t.Fatalf("history length = %d, want 2", len(hist))
	}

	b, err := s.ContentAtRevision("flows/a.yaml", h1)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "v1" {
		t.Errorf("content at rev1 = %q, want v1", b)
	}

	// Modify without committing, then verify diff and restore.
	if err := s.WriteFile("flows/a.yaml", []byte("v3")); err != nil {
		t.Fatal(err)
	}
	diff, err := s.WorkingTreeDiff()
	if err != nil {
		t.Fatal(err)
	}
	if len(diff) == 0 {
		t.Error("expected non-empty working-tree diff")
	}

	if err := s.Restore("flows/a.yaml", h1); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadFile("flows/a.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v1" {
		t.Errorf("after restore = %q, want v1", got)
	}
}

// TestOpenExistingRepository ensures a store initialized on disk can be
// reopened later (e.g. after a process restart) and still reflects prior
// commits and working-tree content.
func TestOpenExistingRepository(t *testing.T) {
	dir := t.TempDir()

	created, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := created.WriteFile("flows/a.yaml", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	if _, err := created.Commit("add a", Author{Name: "tester", Email: "t@example.com"}); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := reopened.ReadFile("flows/a.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v1" {
		t.Errorf("reopened content = %q, want v1", got)
	}
	log, err := reopened.Log()
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 1 {
		t.Fatalf("reopened log length = %d, want 1", len(log))
	}
}

// TestOpenNonexistentRepositoryFails ensures Open surfaces an error rather
// than panicking or silently returning a zero-value store when the path is
// not a git repository.
func TestOpenNonexistentRepositoryFails(t *testing.T) {
	dir := t.TempDir()
	if _, err := Open(dir); err == nil {
		t.Error("expected error opening a non-git directory")
	}
}

// TestLogAndHistoryOnEmptyRepository ensures Log and History return an
// empty result (not an error) for a freshly initialized repository with no
// commits yet, exercising the ErrReferenceNotFound branch.
func TestLogAndHistoryOnEmptyRepository(t *testing.T) {
	s, err := NewMem()
	if err != nil {
		t.Fatal(err)
	}

	log, err := s.Log()
	if err != nil {
		t.Fatalf("Log on empty repo: %v", err)
	}
	if len(log) != 0 {
		t.Errorf("Log on empty repo = %d entries, want 0", len(log))
	}

	hist, err := s.History("flows/a.yaml")
	if err != nil {
		t.Fatalf("History on empty repo: %v", err)
	}
	if len(hist) != 0 {
		t.Errorf("History on empty repo = %d entries, want 0", len(hist))
	}
}

// TestHistoryIgnoresUnrelatedPaths ensures History filters by path and
// excludes commits that never touched the given file.
func TestHistoryIgnoresUnrelatedPaths(t *testing.T) {
	s, err := NewMem()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WriteFile("flows/a.yaml", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Commit("add a", Author{Name: "t", Email: "t@example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteFile("flows/b.yaml", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Commit("add b", Author{Name: "t", Email: "t@example.com"}); err != nil {
		t.Fatal(err)
	}

	hist, err := s.History("flows/a.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 {
		t.Fatalf("history for a.yaml = %d entries, want 1", len(hist))
	}
	if hist[0].Message != "add a" {
		t.Errorf("history message = %q, want %q", hist[0].Message, "add a")
	}
}

func TestPushPullRemoteWins(t *testing.T) {
	dir := t.TempDir()
	remoteDir := filepath.Join(dir, "remote.git")
	if _, err := git.PlainInit(remoteDir, true); err != nil {
		t.Fatal(err)
	}

	aDir := filepath.Join(dir, "a")
	a, err := Init(aDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.CreateRemote("origin", remoteDir); err != nil {
		t.Fatal(err)
	}
	if err := a.WriteFile("flows/a.yaml", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Commit("init", Author{Name: "t", Email: "t@example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Push("origin"); err != nil {
		t.Fatalf("push: %v", err)
	}

	bDir := filepath.Join(dir, "b")
	bRepo, err := git.PlainClone(bDir, false, &git.CloneOptions{URL: remoteDir})
	if err != nil {
		t.Fatal(err)
	}
	b, err := openFrom(bRepo)
	if err != nil {
		t.Fatal(err)
	}

	// A commits and pushes; B pulls (remote wins).
	if err := a.WriteFile("flows/a.yaml", []byte("v2")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Commit("v2", Author{Name: "t", Email: "t@example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Push("origin"); err != nil {
		t.Fatalf("push v2: %v", err)
	}

	if err := b.Pull("origin"); err != nil {
		t.Fatalf("pull: %v", err)
	}
	got, err := b.ReadFile("flows/a.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v2" {
		t.Errorf("B file = %q, want v2 (remote-wins)", got)
	}
}
