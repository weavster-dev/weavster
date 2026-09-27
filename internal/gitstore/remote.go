package gitstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
)

func init() {
	// Local-path and file:// remotes are served in process, so the single
	// binary needs no git executable.
	client.InstallProtocol("file", localServer{server.DefaultServer, server.DefaultLoader})
}

// localServer is go-git's in-process server, except that a fetch may offer
// commits the remote does not have (a diverged branch): like git, the
// remote ignores them instead of failing with "object not found".
type localServer struct {
	transport.Transport
	loader server.Loader
}

func (l localServer) NewUploadPackSession(ep *transport.Endpoint, auth transport.AuthMethod) (transport.UploadPackSession, error) {
	sess, err := l.Transport.NewUploadPackSession(ep, auth)
	if err != nil {
		return nil, err
	}
	st, err := l.loader.Load(ep)
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	return knownHaves{sess, st}, nil
}

type knownHaves struct {
	transport.UploadPackSession
	st storer.EncodedObjectStorer
}

func (k knownHaves) UploadPack(ctx context.Context, req *packp.UploadPackRequest) (*packp.UploadPackResponse, error) {
	known := make([]plumbing.Hash, 0, len(req.Haves))
	for _, h := range req.Haves {
		if k.st.HasEncodedObject(h) == nil {
			known = append(known, h)
		}
	}
	req.Haves = known
	return k.UploadPackSession.UploadPack(ctx, req)
}

// remoteName is the one remote the server uses.
const remoteName = "origin"

// Remote is the remote repository and its HTTPS credentials (empty: none).
type Remote struct {
	URL, Username, Password string
}

// ErrRejected: the remote has commits this repository does not have.
var ErrRejected = errors.New("the remote has commits this repository does not have")

// ErrNothingToPush: the repository has no commits yet.
var ErrNothingToPush = errors.New("nothing to push: the repository has no commits")

// ErrDetached: HEAD is not on a branch, so there is no branch to push or
// pull.
var ErrDetached = errors.New("HEAD is not on a branch; check out a branch in the repository")

// ErrUncommitted: the working tree has changes a pull would overwrite.
var ErrUncommitted = errors.New("the repository has uncommitted changes; commit them (or remove them) before pulling")

// ErrRemote marks a failure talking to the remote (unreachable, refused,
// timed out), as opposed to a local repository error.
var ErrRemote = errors.New("remote")

func (r Remote) auth() transport.AuthMethod {
	if r.Username == "" && r.Password == "" {
		return nil
	}
	return &http.BasicAuth{Username: r.Username, Password: r.Password}
}

// setRemote points origin at r.URL, writing the repository configuration
// only when it changes.
func (s *Store) setRemote(r Remote) error {
	cfg, err := s.repo.Config()
	if err != nil {
		return err
	}
	if old, ok := cfg.Remotes[remoteName]; ok && len(old.URLs) == 1 && old.URLs[0] == r.URL {
		return nil
	}
	cfg.Remotes[remoteName] = &config.RemoteConfig{
		Name: remoteName, URLs: []string{r.URL},
		Fetch: []config.RefSpec{"+refs/heads/*:refs/remotes/" + remoteName + "/*"},
	}
	return s.repo.SetConfig(cfg)
}

// fetch updates the remote-tracking branches from r, dropping those whose
// branch is gone from the remote.
func (s *Store) fetch(ctx context.Context, r Remote) error {
	if err := s.setRemote(r); err != nil {
		return err
	}
	err := s.repo.FetchContext(ctx, &git.FetchOptions{RemoteName: remoteName, Auth: r.auth(), Force: true, Prune: true})
	if err == nil || errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil // an empty remote prunes every tracking branch too
	}
	return fmt.Errorf("%w: %w", ErrRemote, err)
}

// RemoteState compares the current branch with the same branch on the
// remote (as last fetched): Ahead lists the local commits the remote
// lacks, newest first; Behind counts the remote's commits missing locally.
type RemoteState struct {
	Branch, Head, RemoteHead string // "" when there is no commit
	Ahead                    []string
	Behind                   int
}

// Status fetches from r and compares.
func (s *Store) Status(ctx context.Context, r Remote) (RemoteState, error) {
	if err := s.fetch(ctx, r); err != nil {
		return RemoteState{}, err
	}
	return s.State()
}

// State compares the current branch with the remote's as last fetched,
// without contacting the remote.
func (s *Store) State() (RemoteState, error) {
	head, branch, err := s.Head()
	if err != nil {
		return RemoteState{}, err
	}
	if branch == "" {
		return RemoteState{}, ErrDetached
	}
	st := RemoteState{Branch: branch, Head: head, Ahead: []string{}}
	ref, err := s.repo.Reference(plumbing.NewRemoteReferenceName(remoteName, branch), true)
	if err == nil {
		st.RemoteHead = ref.Hash().String()
	} else if !errors.Is(err, plumbing.ErrReferenceNotFound) {
		return RemoteState{}, err
	}
	local, err := s.ancestors(st.Head)
	if err != nil {
		return RemoteState{}, err
	}
	remote, err := s.ancestors(st.RemoteHead)
	if err != nil {
		return RemoteState{}, err
	}
	inRemote := make(map[string]bool, len(remote))
	for _, h := range remote {
		inRemote[h] = true
	}
	inLocal := make(map[string]bool, len(local))
	for _, h := range local {
		inLocal[h] = true
		if !inRemote[h] {
			st.Ahead = append(st.Ahead, h)
		}
	}
	for _, h := range remote {
		if !inLocal[h] {
			st.Behind++
		}
	}
	return st, nil
}

// ancestors lists hash and its ancestors, newest first ("" has none).
func (s *Store) ancestors(hash string) ([]string, error) {
	if hash == "" {
		return nil, nil
	}
	iter, err := s.repo.Log(&git.LogOptions{From: plumbing.NewHash(hash)})
	if err != nil {
		return nil, err
	}
	var out []string
	err = iter.ForEach(func(c *object.Commit) error {
		out = append(out, c.Hash.String())
		return nil
	})
	return out, err
}

// PushTo sends the current branch to r (ErrRejected when the remote has
// commits this repository lacks) and records the remote-tracking branch
// as pushed, so State reports the result without fetching again.
func (s *Store) PushTo(ctx context.Context, r Remote) error {
	head, branch, err := s.Head()
	if err != nil {
		return err
	}
	switch {
	case branch == "":
		return ErrDetached
	case head == "":
		return ErrNothingToPush
	}
	if err := s.setRemote(r); err != nil {
		return err
	}
	spec := config.RefSpec("refs/heads/" + branch + ":refs/heads/" + branch)
	err = s.repo.PushContext(ctx, &git.PushOptions{RemoteName: remoteName, Auth: r.auth(), RefSpecs: []config.RefSpec{spec}})
	switch {
	case err == nil, errors.Is(err, git.NoErrAlreadyUpToDate):
	case strings.Contains(err.Error(), "non-fast-forward"): // go-git does not wrap a sentinel
		return ErrRejected
	default:
		return fmt.Errorf("%w: %w", ErrRemote, err)
	}
	tracking := plumbing.NewHashReference(plumbing.NewRemoteReferenceName(remoteName, branch), plumbing.NewHash(head))
	return s.repo.Storer.SetReference(tracking)
}

// PullRemoteWins fetches from r and resets the current branch, the index,
// and the working tree to the remote branch: the remote wins (spec
// §2.12.40). It returns the local commits that were dropped, newest first;
// ErrNotFound when the remote has no such branch. It refuses to run over
// uncommitted changes (ErrUncommitted), so if the reset fails the branch
// and working tree can be put back exactly as committed.
func (s *Store) PullRemoteWins(ctx context.Context, r Remote) (dropped []string, err error) {
	changed, err := s.WorkingTreeDiff()
	if err != nil {
		return nil, err
	}
	if len(changed) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrUncommitted, strings.Join(changed, ", "))
	}
	if err := s.fetch(ctx, r); err != nil {
		return nil, err
	}
	st, err := s.State()
	if err != nil {
		return nil, err
	}
	if st.RemoteHead == "" {
		return nil, ErrNotFound
	}
	branch := plumbing.NewBranchReferenceName(st.Branch)
	target := plumbing.NewHash(st.RemoteHead)
	if err := s.repo.Storer.SetReference(plumbing.NewHashReference(branch, target)); err != nil {
		return nil, err
	}
	if err := s.wt.Reset(&git.ResetOptions{Commit: target, Mode: git.HardReset}); err != nil {
		if rbErr := s.restore(branch, st.Head); rbErr != nil {
			return nil, fmt.Errorf("pull: %w; putting the repository back also failed: %w", err, rbErr)
		}
		return nil, err
	}
	return st.Ahead, nil
}

// restore points branch back at head ("" = no commit yet) and resets the
// index and working tree to it.
func (s *Store) restore(branch plumbing.ReferenceName, head string) error {
	if head == "" {
		return s.repo.Storer.RemoveReference(branch)
	}
	h := plumbing.NewHash(head)
	if err := s.repo.Storer.SetReference(plumbing.NewHashReference(branch, h)); err != nil {
		return err
	}
	return s.wt.Reset(&git.ResetOptions{Commit: h, Mode: git.HardReset})
}
