package gitstore

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/object"
)

// FileChange is one changed file.
type FileChange struct {
	Path   string
	Status string // added, modified, or deleted
}

// MaxPatchBytes bounds the patch Diff returns.
const MaxPatchBytes = 5 << 20

// treeAt returns the tree of revision rev (ErrNotFound when there is no
// such revision).
func (s *Store) treeAt(rev string) (*object.Tree, error) {
	c, err := s.resolve(rev)
	if err != nil {
		return nil, err
	}
	return c.Tree()
}

// changes compares two trees without rename detection, so a moved file is
// a deletion and an addition.
func changes(ctx context.Context, a, b *object.Tree) (object.Changes, []FileChange, error) {
	ch, err := object.DiffTreeWithOptions(ctx, a, b, &object.DiffTreeOptions{})
	if err != nil {
		return nil, nil, err
	}
	out := make([]FileChange, 0, len(ch))
	for _, c := range ch {
		switch {
		case c.From.Name == "":
			out = append(out, FileChange{Path: c.To.Name, Status: "added"})
		case c.To.Name == "":
			out = append(out, FileChange{Path: c.From.Name, Status: "deleted"})
		default:
			out = append(out, FileChange{Path: c.To.Name, Status: "modified"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return ch, out, nil
}

// Diff compares two revisions: the files changed from from to to, and the
// unified patch (cut at MaxPatchBytes; truncated reports it). ErrNotFound
// when either revision does not exist.
func (s *Store) Diff(ctx context.Context, from, to string) (files []FileChange, patch string, truncated bool, err error) {
	at, err := s.treeAt(from)
	if err != nil {
		return nil, "", false, err
	}
	bt, err := s.treeAt(to)
	if err != nil {
		return nil, "", false, err
	}
	ch, files, err := changes(ctx, at, bt)
	if err != nil {
		return nil, "", false, err
	}
	p, err := ch.PatchContext(ctx)
	if err != nil {
		return nil, "", false, err
	}
	patch = p.String()
	if len(patch) > MaxPatchBytes {
		patch, truncated = patch[:MaxPatchBytes], true
	}
	return files, patch, truncated, nil
}

// WorkingChanges lists the files whose working-tree content differs from
// HEAD: added (not in HEAD), deleted (in HEAD, not on disk), or modified.
// Ignored files are not listed.
func (s *Store) WorkingChanges() ([]FileChange, error) {
	st, err := s.wt.Status()
	if err != nil {
		return nil, err
	}
	head, err := s.treeAt("HEAD")
	if errors.Is(err, ErrNotFound) {
		head, err = nil, nil // no commit yet: everything is added
	}
	if err != nil {
		return nil, err
	}
	out := make([]FileChange, 0)
	for path := range st { // Status lists only changed files
		inHead := false
		if head != nil {
			_, err := head.File(path)
			inHead = err == nil
		}
		_, statErr := s.fs.Stat(path)
		onDisk := statErr == nil
		if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
			return nil, statErr
		}
		switch {
		case !inHead && onDisk:
			out = append(out, FileChange{Path: path, Status: "added"})
		case inHead && !onDisk:
			out = append(out, FileChange{Path: path, Status: "deleted"})
		case inHead:
			out = append(out, FileChange{Path: path, Status: "modified"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// RestoreTo records revision rev's content — one file when path is set,
// otherwise the whole tree — as a new commit on top of HEAD. Only the files
// that differ are written; other files (ignored or untracked ones
// included) are never touched, and on failure the files written are put
// back. It returns the new head and the changed files ("" and none when
// nothing changed); ErrUncommitted over uncommitted changes, ErrNotFound
// for an unknown revision or a path that did not exist at it.
func (s *Store) RestoreTo(ctx context.Context, rev, path, message string, author Author) (string, []string, error) {
	if changed, err := s.WorkingTreeDiff(); err != nil {
		return "", nil, err
	} else if len(changed) > 0 {
		return "", nil, fmt.Errorf("%w: %s", ErrUncommitted, strings.Join(changed, ", "))
	}
	if path != "" {
		content, err := s.ContentAtRevision(path, rev)
		if err != nil {
			return "", nil, err
		}
		return s.writeAndCommit(map[string][]byte{path: content}, message, author)
	}
	targetTree, err := s.treeAt(rev)
	if err != nil {
		return "", nil, err
	}
	curTree, err := s.treeAt("HEAD")
	if err != nil {
		return "", nil, err
	}
	_, diff, err := changes(ctx, curTree, targetTree)
	if err != nil {
		return "", nil, err
	}
	want := map[string][]byte{} // nil: delete
	for _, c := range diff {
		if c.Status == "deleted" {
			want[c.Path] = nil
			continue
		}
		f, err := targetTree.File(c.Path)
		if err != nil {
			return "", nil, err
		}
		content, err := f.Contents()
		if err != nil {
			return "", nil, err
		}
		want[c.Path] = []byte(content)
	}
	return s.writeAndCommit(want, message, author)
}

// writeAndCommit writes (nil: deletes) the files and commits them when
// anything changed; on failure it puts the files back and unstages.
func (s *Store) writeAndCommit(want map[string][]byte, message string, author Author) (head string, changed []string, err error) {
	backup := map[string][]byte{} // nil: did not exist
	defer func() {
		if err == nil {
			return
		}
		for f, b := range backup {
			if b == nil {
				_ = s.RemoveFile(f)
			} else {
				_ = s.WriteFile(f, b)
			}
		}
		if rbErr := s.Unstage(); rbErr != nil {
			err = fmt.Errorf("restore: %w; putting the repository back also failed: %w", err, rbErr)
		}
	}()
	for f, content := range want {
		old, readErr := s.ReadFile(f)
		if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
			return "", nil, readErr
		}
		backup[f] = old
		if content == nil {
			err = s.RemoveFile(f)
		} else {
			err = s.WriteFile(f, content)
		}
		if err != nil {
			return "", nil, err
		}
	}
	if changed, err = s.WorkingTreeDiff(); err != nil || len(changed) == 0 {
		return "", nil, err
	}
	head, err = s.Commit(message, author)
	return head, changed, err
}
