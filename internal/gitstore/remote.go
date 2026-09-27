package gitstore

import (
	"context"
	"errors"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
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

func (r Remote) auth() transport.AuthMethod {
	if r.Username == "" && r.Password == "" {
		return nil
	}
	return &http.BasicAuth{Username: r.Username, Password: r.Password}
}

// setRemote points origin at r.URL.
func (s *Store) setRemote(r Remote) error {
	cfg, err := s.repo.Config()
	if err != nil {
		return err
	}
	cfg.Remotes[remoteName] = &config.RemoteConfig{
		Name: remoteName, URLs: []string{r.URL},
		Fetch: []config.RefSpec{"+refs/heads/*:refs/remotes/" + remoteName + "/*"},
	}
	return s.repo.SetConfig(cfg)
}

// fetch updates the remote-tracking branches from r.
func (s *Store) fetch(r Remote) error {
	if err := s.setRemote(r); err != nil {
		return err
	}
	err := s.repo.Fetch(&git.FetchOptions{RemoteName: remoteName, Auth: r.auth(), Force: true})
	if errors.Is(err, git.NoErrAlreadyUpToDate) || errors.Is(err, transport.ErrEmptyRemoteRepository) {
		return nil
	}
	return err
}

// RemoteState compares the current branch with the same branch on the
// remote (after fetching): Ahead lists the local commits the remote lacks,
// newest first; Behind counts the remote's commits missing locally.
type RemoteState struct {
	Branch, Head, RemoteHead string // "" when there is no commit
	Ahead                    []string
	Behind                   int
}

// Status fetches from r and compares.
func (s *Store) Status(r Remote) (RemoteState, error) {
	if err := s.fetch(r); err != nil {
		return RemoteState{}, err
	}
	return s.state()
}

func (s *Store) state() (RemoteState, error) {
	head, branch, err := s.Head()
	if err != nil {
		return RemoteState{}, err
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
	for _, h := range local {
		if !contains(remote, h) {
			st.Ahead = append(st.Ahead, h)
		}
	}
	for _, h := range remote {
		if !contains(local, h) {
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
	revs, err := revisions(iter, 0)
	out := make([]string, len(revs))
	for i, r := range revs {
		out[i] = r.Hash
	}
	return out, err
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// PushTo sends the current branch to r; ErrRejected when the remote has
// commits this repository lacks.
func (s *Store) PushTo(r Remote) error {
	head, branch, err := s.Head()
	if err != nil {
		return err
	}
	if head == "" {
		return ErrNothingToPush
	}
	if err := s.setRemote(r); err != nil {
		return err
	}
	spec := config.RefSpec("refs/heads/" + branch + ":refs/heads/" + branch)
	err = s.repo.Push(&git.PushOptions{RemoteName: remoteName, Auth: r.auth(), RefSpecs: []config.RefSpec{spec}})
	switch {
	case errors.Is(err, git.NoErrAlreadyUpToDate):
		err = nil
	case err != nil && strings.Contains(err.Error(), "non-fast-forward"): // go-git does not wrap a sentinel
		return ErrRejected
	}
	if err != nil {
		return err
	}
	return s.fetch(r) // the remote-tracking branch now matches
}

// PullRemoteWins fetches from r and resets the current branch, the index,
// and the working tree to the remote branch: the remote wins (spec
// §2.12.40). It returns the local commits that were dropped, newest first;
// ErrNotFound when the remote has no such branch.
func (s *Store) PullRemoteWins(r Remote) (dropped []string, err error) {
	if err := s.fetch(r); err != nil {
		return nil, err
	}
	st, err := s.state()
	if err != nil {
		return nil, err
	}
	if st.RemoteHead == "" {
		return nil, ErrNotFound
	}
	target := plumbing.NewHash(st.RemoteHead)
	if err := s.repo.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName(st.Branch), target)); err != nil {
		return nil, err
	}
	if err := s.wt.Reset(&git.ResetOptions{Commit: target, Mode: git.HardReset}); err != nil {
		return nil, err
	}
	return st.Ahead, nil
}
