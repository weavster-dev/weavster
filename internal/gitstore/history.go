package gitstore

import (
	"errors"
	"time"

	"github.com/go-git/go-git/v5/plumbing/storer"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Revision is one historical revision of the repository (spec §2.12.39).
type Revision struct {
	Hash    string
	Message string
	Author  string
	When    time.Time
}

// Log returns the repository commit log (newest first).
func (s *Store) Log() ([]Revision, error) { return s.Revisions("", 0) }

// History returns commits that touched path (newest first).
func (s *Store) History(path string) ([]Revision, error) { return s.Revisions(path, 0) }

// Revisions returns the newest limit commits (0 = all), newest first; with
// path only those that touched it. The walk stops at limit.
func (s *Store) Revisions(path string, limit int) ([]Revision, error) {
	opts := &git.LogOptions{}
	if path != "" {
		opts.FileName = &path
	}
	iter, err := s.repo.Log(opts)
	if err != nil {
		if err == plumbing.ErrReferenceNotFound {
			return nil, nil
		}
		return nil, err
	}
	return revisions(iter, limit)
}

// ContentAtRevision returns the content of path at the given revision (any
// revision Git understands); ErrNotFound when either does not exist.
func (s *Store) ContentAtRevision(path, rev string) ([]byte, error) {
	tree, err := s.treeAt(rev)
	if err != nil {
		return nil, err
	}
	f, err := tree.File(path)
	if errors.Is(err, object.ErrFileNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	content, err := f.Contents()
	if err != nil {
		return nil, err
	}
	return []byte(content), nil
}

func revisions(iter object.CommitIter, limit int) ([]Revision, error) {
	var out []Revision
	err := iter.ForEach(func(c *object.Commit) error {
		if limit > 0 && len(out) == limit {
			return storer.ErrStop
		}
		out = append(out, Revision{
			Hash:    c.Hash.String(),
			Message: c.Message,
			Author:  c.Author.Name,
			When:    c.Author.When,
		})
		return nil
	})
	return out, err
}
