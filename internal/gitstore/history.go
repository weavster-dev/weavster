package gitstore

import (
	"time"

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
func (s *Store) Log() ([]Revision, error) {
	iter, err := s.repo.Log(&git.LogOptions{})
	if err != nil {
		if err == plumbing.ErrReferenceNotFound {
			return nil, nil
		}
		return nil, err
	}
	return revisions(iter)
}

// History returns commits that touched path (newest first).
func (s *Store) History(path string) ([]Revision, error) {
	iter, err := s.repo.Log(&git.LogOptions{FileName: &path})
	if err != nil {
		if err == plumbing.ErrReferenceNotFound {
			return nil, nil
		}
		return nil, err
	}
	return revisions(iter)
}

// ContentAtRevision returns the content of path at the given revision.
func (s *Store) ContentAtRevision(path, rev string) ([]byte, error) {
	c, err := s.repo.CommitObject(plumbing.NewHash(rev))
	if err != nil {
		return nil, err
	}
	tree, err := c.Tree()
	if err != nil {
		return nil, err
	}
	f, err := tree.File(path)
	if err != nil {
		return nil, err
	}
	content, err := f.Contents()
	if err != nil {
		return nil, err
	}
	return []byte(content), nil
}

func revisions(iter object.CommitIter) ([]Revision, error) {
	var out []Revision
	err := iter.ForEach(func(c *object.Commit) error {
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
