package gitstore

import (
	"testing"
)

// TestOpenReopensExistingRepo covers Open, which reopens an on-disk
// repository that was previously created with Init (e.g. after a process
// restart). It must see all previously committed history and content.
func TestOpenReopensExistingRepo(t *testing.T) {
	dir := t.TempDir()

	s1, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.WriteFile("flows/a.yaml", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	if _, err := s1.Commit("init", Author{Name: "t", Email: "t@example.com"}); err != nil {
		t.Fatal(err)
	}

	// Simulate a process restart: reopen the same on-disk path.
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	log, err := s2.Log()
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 1 {
		t.Fatalf("log length = %d, want 1", len(log))
	}

	got, err := s2.ReadFile("flows/a.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v1" {
		t.Errorf("content = %q, want v1", got)
	}
}

// TestOpenNonexistentRepo covers the error path of Open when the path is not
// a git repository.
func TestOpenNonexistentRepo(t *testing.T) {
	if _, err := Open(t.TempDir()); err == nil {
		t.Error("expected error opening a non-repo directory")
	}
}

// TestLogHistoryEmptyRepo covers Log and History on a freshly initialized
// repository with no commits (the HEAD reference does not exist yet), which
// must return an empty result rather than an error.
func TestLogHistoryEmptyRepo(t *testing.T) {
	s, err := NewMem()
	if err != nil {
		t.Fatal(err)
	}

	log, err := s.Log()
	if err != nil {
		t.Fatalf("Log on empty repo: %v", err)
	}
	if len(log) != 0 {
		t.Errorf("log length = %d, want 0", len(log))
	}

	hist, err := s.History("flows/a.yaml")
	if err != nil {
		t.Fatalf("History on empty repo: %v", err)
	}
	if len(hist) != 0 {
		t.Errorf("history length = %d, want 0", len(hist))
	}
}
