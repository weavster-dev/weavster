package gitstore

import (
	"sort"

	git "github.com/go-git/go-git/v5"
)

// WorkingTreeDiff returns the paths changed in the working tree relative to
// HEAD (spec §2.12.40).
func (s *Store) WorkingTreeDiff() ([]string, error) {
	st, err := s.wt.Status()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0)
	for p, sc := range st {
		if sc.Staging != git.Unmodified || sc.Worktree != git.Unmodified {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Restore restores path to its content at the given revision (spec §2.12.40).
func (s *Store) Restore(path, rev string) error {
	b, err := s.ContentAtRevision(path, rev)
	if err != nil {
		return err
	}
	return s.WriteFile(path, b)
}
