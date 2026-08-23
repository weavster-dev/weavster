// Package gitstore implements the native Git-backed config store (commit,
// push, pull, history, working-tree diff, restore).
package gitstore

import (
	"io"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
)

// Author identifies a commit author.
type Author struct {
	Name  string
	Email string
}

func (a Author) signature() *object.Signature {
	return &object.Signature{Name: a.Name, Email: a.Email, When: time.Now()}
}

// Store is a Git-backed config store (spec §2.10.34, §2.12).
type Store struct {
	repo *git.Repository
	wt   *git.Worktree
	fs   billy.Filesystem
}

// Init creates a new on-disk repository at path.
func Init(path string) (*Store, error) {
	repo, err := git.PlainInit(path, false)
	if err != nil {
		return nil, err
	}
	return openFrom(repo)
}

// Open opens an existing on-disk repository at path.
func Open(path string) (*Store, error) {
	repo, err := git.PlainOpen(path)
	if err != nil {
		return nil, err
	}
	return openFrom(repo)
}

// NewMem returns an in-memory repository (for tests and local DX).
func NewMem() (*Store, error) {
	repo, err := git.Init(memory.NewStorage(), memfs.New())
	if err != nil {
		return nil, err
	}
	return openFrom(repo)
}

func openFrom(repo *git.Repository) (*Store, error) {
	wt, err := repo.Worktree()
	if err != nil {
		return nil, err
	}
	return &Store{repo: repo, wt: wt, fs: wt.Filesystem}, nil
}

// WriteFile writes a file into the working tree.
func (s *Store) WriteFile(path string, content []byte) error {
	f, err := s.fs.Create(path)
	if err != nil {
		return err
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// ReadFile reads a file from the working tree.
func (s *Store) ReadFile(path string) ([]byte, error) {
	f, err := s.fs.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}

// Commit stages and commits all working-tree changes, returning the commit hash.
func (s *Store) Commit(message string, author Author) (string, error) {
	if _, err := s.wt.Add("."); err != nil {
		return "", err
	}
	h, err := s.wt.Commit(message, &git.CommitOptions{Author: author.signature()})
	if err != nil {
		return "", err
	}
	return h.String(), nil
}

// CreateRemote registers a named remote.
func (s *Store) CreateRemote(name, url string) error {
	_, err := s.repo.CreateRemote(&config.RemoteConfig{Name: name, URLs: []string{url}})
	return err
}

// Push pushes the current branch to the named remote.
func (s *Store) Push(remote string) error {
	return s.repo.Push(&git.PushOptions{RemoteName: remote})
}

// Pull fetches and resets to the named remote (remote-wins conflict policy,
// spec §2.12.40).
func (s *Store) Pull(remote string) error {
	err := s.wt.Pull(&git.PullOptions{RemoteName: remote, Force: true})
	if err == git.NoErrAlreadyUpToDate {
		return nil
	}
	return err
}
