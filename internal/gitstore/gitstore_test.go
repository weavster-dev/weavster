package gitstore

import (
	"os"
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

// TestInitOnRegularFileFails ensures Init surfaces the underlying error
// rather than panicking when path already exists as a non-directory file
// (e.g. a stray file blocking repo creation on disk).
func TestInitOnRegularFileFails(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x")
	if err := os.WriteFile(f, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(f); err == nil {
		t.Error("expected error initializing a repo at a regular file path")
	}
}

// TestReadFileMissingFails ensures ReadFile returns an error for a path
// that does not exist in the working tree, instead of a zero-value slice.
func TestReadFileMissingFails(t *testing.T) {
	s, err := NewMem()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadFile("does-not-exist.yaml"); err == nil {
		t.Error("expected error reading a missing file")
	}
}

// TestContentAtRevisionBadHashFails ensures ContentAtRevision surfaces an
// error when given a revision hash that does not exist in the repository.
func TestContentAtRevisionBadHashFails(t *testing.T) {
	s, err := NewMem()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ContentAtRevision("a.txt", "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"); err == nil {
		t.Error("expected error for a nonexistent revision hash")
	}
}

// TestContentAtRevisionMissingPathFails ensures ContentAtRevision surfaces
// an error when the path did not exist at the given (real) revision.
func TestContentAtRevisionMissingPathFails(t *testing.T) {
	s, err := NewMem()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WriteFile("a.txt", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	h, err := s.Commit("add a", Author{Name: "tester", Email: "t@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ContentAtRevision("never-existed.yaml", h); err == nil {
		t.Error("expected error for a path absent at the given revision")
	}
}

// TestPullAlreadyUpToDateReturnsNil ensures Pull maps go-git's
// NoErrAlreadyUpToDate sentinel to a nil error (rather than surfacing it as
// a failure) when a second pull finds nothing new to fetch.
func TestPullAlreadyUpToDateReturnsNil(t *testing.T) {
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
	if err := a.WriteFile("f.txt", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Commit("init", Author{Name: "t", Email: "t@example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Push("origin"); err != nil {
		t.Fatal(err)
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

	// Nothing new has been pushed since the clone, so this pull should hit
	// the NoErrAlreadyUpToDate branch and return nil (not an error).
	if err := b.Pull("origin"); err != nil {
		t.Errorf("pull with nothing new = %v, want nil", err)
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
