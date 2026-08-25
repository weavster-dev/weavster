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

func TestInitAndOpen(t *testing.T) {
	dir := t.TempDir()
	s, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WriteFile("file.txt", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Commit("init", Author{Name: "tester", Email: "t@example.com"}); err != nil {
		t.Fatal(err)
	}

	// Re-open the on-disk repository.
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	data, err := s2.ReadFile("file.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("file content = %q", data)
	}
}

func TestLogAndHistoryEmptyAndMissing(t *testing.T) {
	s, err := NewMem()
	if err != nil {
		t.Fatal(err)
	}

	// Log on repo with no commits should return empty or ErrNoHead.
	log, err := s.Log()
	if err != nil {
		// ErrNoHead is acceptable on an empty repo.
		if log != nil {
			t.Errorf("expected nil log on error, got %v", log)
		}
	} else if len(log) != 0 {
		t.Errorf("log on empty repo = %d, want 0", len(log))
	}

	// History for a missing path after one commit.
	if err := s.WriteFile("a.yaml", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Commit("add a", Author{Name: "tester", Email: "t@example.com"}); err != nil {
		t.Fatal(err)
	}
	hist, err := s.History("noexist.yaml")
	if err != nil {
		// Allow not-found error.
		_ = err
	} else if len(hist) != 0 {
		t.Errorf("expected empty history for missing file, got %d entries", len(hist))
	}
}
