package gitstore

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/go-git/go-billy/v5/util"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// ErrNotFound: the revision or the path at that revision does not exist.
var ErrNotFound = errors.New("not found")

// OpenOrInit opens the repository at path, creating the directory and an
// empty repository (default branch main) when there is none.
func OpenOrInit(path string) (*Store, error) {
	repo, err := git.PlainOpen(path)
	if errors.Is(err, git.ErrRepositoryNotExists) {
		if err := os.MkdirAll(path, 0o750); err != nil {
			return nil, err
		}
		repo, err = git.PlainInitWithOptions(path, &git.PlainInitOptions{InitOptions: git.InitOptions{DefaultBranch: plumbing.Main}})
	}
	if err != nil {
		return nil, err
	}
	return openFrom(repo)
}

// RemoveFile deletes a file from the working tree.
func (s *Store) RemoveFile(path string) error {
	return s.fs.Remove(path)
}

// Files lists the working-tree files (slash-separated, sorted), without
// the .git directory.
func (s *Store) Files() ([]string, error) {
	out := make([]string, 0)
	err := util.Walk(s.fs, "", func(p string, info fs.FileInfo, err error) error {
		switch {
		case err != nil:
			return err
		case info.IsDir() && info.Name() == ".git":
			return filepath.SkipDir
		case !info.IsDir():
			out = append(out, filepath.ToSlash(p))
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

// Head returns the current commit hash ("" before the first commit) and
// branch name.
func (s *Store) Head() (hash, branch string, err error) {
	ref, err := s.repo.Reference(plumbing.HEAD, false)
	if err != nil {
		return "", "", err
	}
	branch = ref.Target().Short()
	if resolved, err := s.repo.Head(); err == nil {
		hash = resolved.Hash().String()
	} else if !errors.Is(err, plumbing.ErrReferenceNotFound) {
		return "", "", err
	}
	return hash, branch, nil
}

// resolve turns any revision Git understands (hash, short hash, HEAD~1,
// branch) into a commit; ErrNotFound when there is no such commit, other
// errors (storage) as they are.
func (s *Store) resolve(rev string) (*object.Commit, error) {
	h, err := s.repo.ResolveRevision(plumbing.Revision(rev))
	if err == nil {
		var c *object.Commit
		if c, err = s.repo.CommitObject(*h); err == nil {
			return c, nil
		}
	}
	if errors.Is(err, plumbing.ErrReferenceNotFound) || errors.Is(err, plumbing.ErrObjectNotFound) {
		return nil, ErrNotFound
	}
	return nil, err
}

// Unstage resets the index to HEAD (to empty before the first commit),
// leaving the working tree as it is.
func (s *Store) Unstage() error {
	if _, err := s.repo.Head(); errors.Is(err, plumbing.ErrReferenceNotFound) {
		idx, err := s.repo.Storer.Index()
		if err != nil {
			return err
		}
		idx.Entries = nil
		return s.repo.Storer.SetIndex(idx)
	}
	return s.wt.Reset(&git.ResetOptions{Mode: git.MixedReset})
}

// FilesAt lists the files of revision rev (any revision Git understands),
// sorted; ErrNotFound when there is no such revision.
func (s *Store) FilesAt(rev string) ([]string, error) {
	c, err := s.resolve(rev)
	if err != nil {
		return nil, err
	}
	tree, err := c.Tree()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0)
	err = tree.Files().ForEach(func(f *object.File) error {
		out = append(out, f.Name)
		return nil
	})
	sort.Strings(out)
	return out, err
}
