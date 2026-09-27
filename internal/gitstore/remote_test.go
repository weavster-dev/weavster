package gitstore

import (
	"context"
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

func TestRemotePushPullRemoteWins(t *testing.T) {
	ctx := context.Background()
	remoteDir := filepath.Join(t.TempDir(), "remote.git")
	if _, err := git.PlainInit(remoteDir, true); err != nil {
		t.Fatal(err)
	}
	remote := Remote{URL: remoteDir}
	open := func() *Store {
		t.Helper()
		s, err := OpenOrInit(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	commit := func(s *Store, file, content string) string {
		t.Helper()
		if err := s.WriteFile(file, []byte(content)); err != nil {
			t.Fatal(err)
		}
		h, err := s.Commit(content, Author{Name: "t"})
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	a, b := open(), open()

	// Nothing to push or pull yet.
	if err := a.PushTo(ctx, remote); !errors.Is(err, ErrNothingToPush) {
		t.Errorf("empty push = %v", err)
	}
	if _, err := a.PullRemoteWins(ctx, remote); !errors.Is(err, ErrNotFound) {
		t.Errorf("pull from an empty remote = %v", err)
	}

	first := commit(a, "a.yaml", "one")
	if st, err := a.Status(ctx, remote); err != nil || len(st.Ahead) != 1 || st.RemoteHead != "" {
		t.Errorf("before push = %+v %v", st, err)
	}
	if err := a.PushTo(ctx, remote); err != nil {
		t.Fatal(err)
	}
	if st, err := a.Status(ctx, remote); err != nil || len(st.Ahead) != 0 || st.Behind != 0 || st.RemoteHead != first || st.Branch != "main" {
		t.Errorf("after push = %+v %v", st, err)
	}

	// b takes the remote, commits, and pushes; a commits too: divergence.
	if dropped, err := b.PullRemoteWins(ctx, remote); err != nil || len(dropped) != 0 {
		t.Fatalf("b pull = %v %v", dropped, err)
	}
	theirs := commit(b, "a.yaml", "theirs")
	if err := b.PushTo(ctx, remote); err != nil {
		t.Fatal(err)
	}
	ours := commit(a, "a.yaml", "ours")
	if err := a.PushTo(ctx, remote); !errors.Is(err, ErrRejected) {
		t.Errorf("divergent push = %v", err)
	}
	if st, err := a.Status(ctx, remote); err != nil || len(st.Ahead) != 1 || st.Ahead[0] != ours || st.Behind != 1 {
		t.Errorf("divergent status = %+v %v", st, err)
	}

	// Uncommitted changes block a pull, which would overwrite them.
	if err := a.WriteFile("notes.txt", []byte("draft")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PullRemoteWins(ctx, remote); !errors.Is(err, ErrUncommitted) || !strings.Contains(err.Error(), "notes.txt") {
		t.Errorf("pull over uncommitted = %v", err)
	}
	if err := a.RemoveFile("notes.txt"); err != nil {
		t.Fatal(err)
	}

	// Pull: the remote wins; ours is dropped and reported.
	dropped, err := a.PullRemoteWins(ctx, remote)
	if err != nil || len(dropped) != 1 || dropped[0] != ours {
		t.Fatalf("pull = %v %v", dropped, err)
	}
	if head, _, _ := a.Head(); head != theirs {
		t.Errorf("head = %s, want %s", head, theirs)
	}
	if got, _ := a.ReadFile("a.yaml"); string(got) != "theirs" {
		t.Errorf("working tree = %q", got)
	}
	if changed, _ := a.WorkingTreeDiff(); len(changed) != 0 {
		t.Errorf("working tree not clean: %v", changed)
	}
	// Now a can build on the remote and push.
	commit(a, "a.yaml", "again")
	if err := a.PushTo(ctx, remote); err != nil {
		t.Errorf("push after pull = %v", err)
	}

	// An unreachable remote is an error for every operation.
	missing := Remote{URL: filepath.Join(t.TempDir(), "missing.git")}
	if _, err := a.Status(ctx, missing); err == nil {
		t.Error("status of a missing remote succeeded")
	}
	if err := a.PushTo(ctx, missing); !errors.Is(err, ErrRemote) {
		t.Errorf("push to a missing remote = %v", err)
	}
	if _, err := a.PullRemoteWins(ctx, missing); !errors.Is(err, ErrRemote) {
		t.Errorf("pull from a missing remote = %v", err)
	}
	// An HTTP remote that never answers is given up when the request's
	// context ends.
	hang := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) { <-r.Context().Done() }))
	defer hang.Close()
	short, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if _, err := a.Status(short, Remote{URL: hang.URL + "/r.git"}); !errors.Is(err, ErrRemote) {
		t.Errorf("hanging remote = %v", err)
	}
	if err := a.setRemote(remote); err != nil {
		t.Fatal(err)
	}

	// A branch deleted on the remote is no longer reported, and pull says
	// so instead of resetting to the stale commit.
	bare, err := git.PlainOpen(remoteDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := bare.Storer.RemoveReference(plumbing.NewBranchReferenceName("main")); err != nil {
		t.Fatal(err)
	}
	if st, err := a.Status(ctx, remote); err != nil || st.RemoteHead != "" || len(st.Ahead) == 0 {
		t.Errorf("after remote delete = %+v %v", st, err)
	}
	if _, err := a.PullRemoteWins(ctx, remote); !errors.Is(err, ErrNotFound) {
		t.Errorf("pull after remote delete = %v", err)
	}

	// A detached HEAD has no branch to push or pull.
	head, _, _ := a.Head()
	if err := a.repo.Storer.SetReference(plumbing.NewHashReference(plumbing.HEAD, plumbing.NewHash(head))); err != nil {
		t.Fatal(err)
	}
	if err := a.PushTo(ctx, remote); !errors.Is(err, ErrDetached) {
		t.Errorf("detached push = %v", err)
	}
	if _, err := a.Status(ctx, remote); !errors.Is(err, ErrDetached) {
		t.Errorf("detached status = %v", err)
	}
}

func TestRemoteAuth(t *testing.T) {
	if (Remote{URL: "x"}).auth() != nil {
		t.Error("auth without credentials")
	}
	if a := (Remote{URL: "x", Username: "u", Password: "p"}).auth(); a == nil || a.Name() != "http-basic-auth" {
		t.Errorf("auth = %v", a)
	}
}

// fakeTransport opens sessions that record Close; failLoader cannot load.
type fakeTransport struct {
	transport.Transport
	closed *bool
}

func (f fakeTransport) NewUploadPackSession(*transport.Endpoint, transport.AuthMethod) (transport.UploadPackSession, error) {
	return fakeSession{closed: f.closed}, nil
}

type fakeSession struct {
	transport.UploadPackSession
	closed *bool
}

func (f fakeSession) Close() error { *f.closed = true; return nil }

type failLoader struct{}

func (failLoader) Load(*transport.Endpoint) (storer.Storer, error) {
	return nil, errors.New("no repository")
}

func TestLocalServerLoadFailure(t *testing.T) {
	var closed bool
	ls := localServer{fakeTransport{closed: &closed}, failLoader{}}
	if _, err := ls.NewUploadPackSession(&transport.Endpoint{}, nil); err == nil || !closed {
		t.Errorf("err = %v, closed = %v", err, closed)
	}
}

// TestStateCorruptReferences: a branch or remote-tracking branch naming a
// commit that is not in the repository is an error, not a wrong count.
func TestStateCorruptReferences(t *testing.T) {
	missing := plumbing.NewHash(strings.Repeat("ab", 20))
	for _, ref := range []plumbing.ReferenceName{
		plumbing.NewRemoteReferenceName(remoteName, "main"),
		plumbing.NewBranchReferenceName("main"),
	} {
		s, err := OpenOrInit(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if err := s.WriteFile("a.yaml", []byte("x")); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Commit("c", Author{Name: "t"}); err != nil {
			t.Fatal(err)
		}
		if err := s.repo.Storer.SetReference(plumbing.NewHashReference(ref, missing)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.State(); err == nil {
			t.Errorf("%s: no error", ref)
		}
	}
}

// TestRemoteEmptyAndFailedPull: pointing the remote at an empty repository
// forgets the old remote's branches, and a pull whose reset fails puts the
// branch back.
func TestRemoteEmptyAndFailedPull(t *testing.T) {
	ctx := context.Background()
	bare := func() Remote {
		dir := filepath.Join(t.TempDir(), "r.git")
		if _, err := git.PlainInit(dir, true); err != nil {
			t.Fatal(err)
		}
		return Remote{URL: dir}
	}
	first, second := bare(), bare()
	dir := t.TempDir()
	s, err := OpenOrInit(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WriteFile("a.yaml", []byte("one")); err != nil {
		t.Fatal(err)
	}
	ours, err := s.Commit("one", Author{Name: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PushTo(ctx, first); err != nil {
		t.Fatal(err)
	}
	if st, err := s.Status(ctx, second); err != nil || st.RemoteHead != "" || len(st.Ahead) != 1 {
		t.Errorf("empty remote = %+v %v", st, err)
	}

	// The first remote moves on; the pull cannot write the working tree.
	other, err := OpenOrInit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.PullRemoteWins(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := other.WriteFile("b.yaml", []byte("two")); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Commit("two", Author{Name: "t"}); err != nil {
		t.Fatal(err)
	}
	if err := other.PushTo(ctx, first); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root writes read-only directories")
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0o700) }()
	if _, err := s.PullRemoteWins(ctx, first); err == nil {
		t.Fatal("pull into a read-only working tree succeeded")
	}
	if head, _, _ := s.Head(); head != ours {
		t.Errorf("head after failed pull = %s, want %s", head, ours)
	}
}

// TestRestoreUnbornBranch: putting back a branch that had no commit
// removes it again.
func TestRestoreUnbornBranch(t *testing.T) {
	s, err := OpenOrInit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	branch := plumbing.NewBranchReferenceName("main")
	if err := s.repo.Storer.SetReference(plumbing.NewHashReference(branch, plumbing.NewHash(strings.Repeat("ab", 20)))); err != nil {
		t.Fatal(err)
	}
	if err := s.restore(branch, ""); err != nil {
		t.Fatal(err)
	}
	if head, _, err := s.Head(); err != nil || head != "" {
		t.Errorf("head = %q %v", head, err)
	}
}
