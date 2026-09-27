package gitstore

import (
	"fmt"
	"sort"
	"strings"

	git "github.com/go-git/go-git/v5"
)

// FileChange is one changed file.
type FileChange struct {
	Path   string
	Status string // added, modified, or deleted
}

// Diff compares two revisions: the files changed from from to to, and the
// unified patch. ErrNotFound when either revision does not exist.
func (s *Store) Diff(from, to string) ([]FileChange, string, error) {
	a, err := s.resolve(from)
	if err != nil {
		return nil, "", err
	}
	b, err := s.resolve(to)
	if err != nil {
		return nil, "", err
	}
	patch, err := a.Patch(b)
	if err != nil {
		return nil, "", err
	}
	changes := make([]FileChange, 0)
	for _, fp := range patch.FilePatches() {
		f, t := fp.Files()
		switch {
		case f == nil:
			changes = append(changes, FileChange{Path: t.Path(), Status: "added"})
		case t == nil:
			changes = append(changes, FileChange{Path: f.Path(), Status: "deleted"})
		default:
			changes = append(changes, FileChange{Path: t.Path(), Status: "modified"})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, patch.String(), nil
}

// WorkingChanges lists the working-tree changes against HEAD.
func (s *Store) WorkingChanges() ([]FileChange, error) {
	st, err := s.wt.Status()
	if err != nil {
		return nil, err
	}
	changes := make([]FileChange, 0)
	for path, fs := range st {
		code := fs.Worktree
		if code == git.Unmodified {
			code = fs.Staging
		}
		switch code {
		case git.Untracked, git.Added:
			changes = append(changes, FileChange{Path: path, Status: "added"})
		case git.Deleted:
			changes = append(changes, FileChange{Path: path, Status: "deleted"})
		case git.Unmodified:
		default:
			changes = append(changes, FileChange{Path: path, Status: "modified"})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
}

// RestoreTo makes the working tree match revision rev — one file when path
// is set, otherwise every file — and commits that as a new commit. It
// returns the new head and the files changed ("" and none when nothing
// changed); ErrUncommitted over uncommitted changes, ErrNotFound for an
// unknown revision or a path that did not exist at it. On failure the
// working tree is reset to HEAD (it was clean, so nothing is lost).
func (s *Store) RestoreTo(rev, path, message string, author Author) (head string, changed []string, err error) {
	if changed, err := s.WorkingTreeDiff(); err != nil {
		return "", nil, err
	} else if len(changed) > 0 {
		return "", nil, fmt.Errorf("%w: %s", ErrUncommitted, strings.Join(changed, ", "))
	}
	keep := func(p string) bool { return path == "" || p == path }
	_, files, err := s.ReadAt(rev, keep)
	if err != nil {
		return "", nil, err
	}
	if path != "" && len(files) == 0 {
		return "", nil, ErrNotFound
	}
	defer func() {
		if err != nil {
			if rbErr := s.wt.Reset(&git.ResetOptions{Mode: git.HardReset}); rbErr != nil {
				err = fmt.Errorf("restore: %w; putting the repository back also failed: %w", err, rbErr)
			}
		}
	}()
	if path == "" { // files that did not exist at rev go
		current, err := s.Files()
		if err != nil {
			return "", nil, err
		}
		for _, f := range current {
			if _, ok := files[f]; !ok {
				if err := s.RemoveFile(f); err != nil {
					return "", nil, err
				}
			}
		}
	}
	for f, content := range files {
		if err := s.WriteFile(f, content); err != nil {
			return "", nil, err
		}
	}
	if changed, err = s.WorkingTreeDiff(); err != nil || len(changed) == 0 {
		return "", nil, err
	}
	head, err = s.Commit(message, author)
	return head, changed, err
}
