package gitstore

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-billy/v5/osfs"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"
)

// failWriteFile is a billy.File whose Write always fails. It is used to force
// WriteFile's write-error branch without touching a real filesystem.
type failWriteFile struct{}

func (failWriteFile) Name() string              { return "fail" }
func (failWriteFile) Write([]byte) (int, error) { return 0, errors.New("injected write failure") }
func (failWriteFile) Read([]byte) (int, error)  { return 0, io.EOF }
func (failWriteFile) ReadAt([]byte, int64) (int, error) {
	return 0, io.EOF
}
func (failWriteFile) Seek(int64, int) (int64, error) { return 0, errors.New("seek unsupported") }
func (failWriteFile) Close() error                   { return nil }
func (failWriteFile) Lock() error                    { return nil }
func (failWriteFile) Unlock() error                  { return nil }
func (failWriteFile) Truncate(int64) error           { return nil }

// writeFailFS is a billy.Filesystem whose Create returns a file whose Write
// always fails, so WriteFile surfaces the write error after closing the file.
type writeFailFS struct {
	billy.Filesystem
}

func (f *writeFailFS) Create(string) (billy.File, error) {
	return failWriteFile{}, nil
}

// failReadDirFS is a billy.Filesystem whose ReadDir and Lstat always fail, so
// Worktree.Add cannot traverse the working tree and returns an error.
type failReadDirFS struct {
	billy.Filesystem
}

func (f *failReadDirFS) ReadDir(string) ([]os.FileInfo, error) {
	return nil, os.ErrPermission
}

func (f *failReadDirFS) Lstat(string) (os.FileInfo, error) {
	return nil, os.ErrPermission
}

var errCommitStorage = errors.New("injected commit storage failure")

type commitFailStorage struct {
	*memory.Storage
}

func (s *commitFailStorage) SetEncodedObject(obj plumbing.EncodedObject) (plumbing.Hash, error) {
	if obj.Type() == plumbing.CommitObject {
		return plumbing.ZeroHash, errCommitStorage
	}
	return s.Storage.SetEncodedObject(obj)
}

// TestWriteFileCreateError exercises WriteFile's Create error branch by
// targeting a path whose parent is a regular file, so osfs.Create returns
// ENOTDIR rather than creating the file.
func TestWriteFileCreateError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Store{fs: osfs.New(dir)}
	if err := s.WriteFile("blocker/child.txt", []byte("data")); err == nil {
		t.Fatal("expected error writing through a regular-file parent")
	}
}

// TestWriteFileWriteError exercises WriteFile's write-error branch: when the
// underlying file's Write fails, WriteFile closes the file and returns the
// error.
func TestWriteFileWriteError(t *testing.T) {
	s := &Store{fs: &writeFailFS{Filesystem: memfs.New()}}
	if err := s.WriteFile("any.txt", []byte("data")); err == nil {
		t.Fatal("expected error when the file write fails")
	}
}

// TestCommitAddError exercises Commit's wt.Add error branch: when the working
// tree cannot be traversed (permission denied), Add surfaces the error and
// Commit propagates it without producing a commit.
func TestCommitAddError(t *testing.T) {
	s, err := NewMem()
	if err != nil {
		t.Fatal(err)
	}
	s.wt.Filesystem = &failReadDirFS{Filesystem: s.wt.Filesystem}
	s.fs = s.wt.Filesystem

	if _, err := s.Commit("x", Author{Name: "t", Email: "t@example.com"}); err == nil {
		t.Fatal("expected error when worktree Add fails")
	}
}

// TestCommitOnFreshRepoFails exercises Commit's wt.Add error branch for a
// repository with no HEAD yet: go-git's Worktree.Add returns "entry not found",
// which Commit must surface rather than silently producing a commit.
func TestCommitOnFreshRepoFails(t *testing.T) {
	s, err := NewMem()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Commit("first", Author{Name: "t", Email: "t@example.com"}); err == nil {
		t.Fatal("expected error committing on a repo with no HEAD")
	}
}

func TestCommitStorageErrorPropagates(t *testing.T) {
	repo, err := git.Init(&commitFailStorage{Storage: memory.NewStorage()}, memfs.New())
	if err != nil {
		t.Fatal(err)
	}
	s, err := openFrom(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WriteFile("flow.yaml", []byte("name: admit")); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Commit("add flow", Author{Name: "tester", Email: "t@example.com"}); !errors.Is(err, errCommitStorage) {
		t.Fatalf("Commit error = %v, want wrapped %v", err, errCommitStorage)
	}
}

// TestOpenFromBareRepositoryFails exercises openFrom's error branch: a bare
// repository has no worktree, so repo.Worktree() returns ErrIsBareRepository
// and openFrom must propagate it.
func TestOpenFromBareRepositoryFails(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openFrom(repo); err == nil {
		t.Fatal("expected error opening a bare repository (no worktree)")
	}
}

// TestRestoreBadRevisionError exercises Restore's error branch: restoring from
// a nonexistent revision propagates the underlying ContentAtRevision error.
func TestRestoreBadRevisionError(t *testing.T) {
	s, err := NewMem()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Restore("a.txt", "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"); err == nil {
		t.Fatal("expected error restoring from a nonexistent revision")
	}
}
