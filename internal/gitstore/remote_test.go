package gitstore

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

func TestRemotePushPullRemoteWins(t *testing.T) {
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
	if err := a.PushTo(remote); !errors.Is(err, ErrNothingToPush) {
		t.Errorf("empty push = %v", err)
	}
	if _, err := a.PullRemoteWins(remote); !errors.Is(err, ErrNotFound) {
		t.Errorf("pull from an empty remote = %v", err)
	}

	first := commit(a, "a.yaml", "one")
	if st, err := a.Status(remote); err != nil || len(st.Ahead) != 1 || st.RemoteHead != "" {
		t.Errorf("before push = %+v %v", st, err)
	}
	if err := a.PushTo(remote); err != nil {
		t.Fatal(err)
	}
	if st, err := a.Status(remote); err != nil || len(st.Ahead) != 0 || st.Behind != 0 || st.RemoteHead != first || st.Branch != "main" {
		t.Errorf("after push = %+v %v", st, err)
	}

	// b takes the remote, commits, and pushes; a commits too: divergence.
	if dropped, err := b.PullRemoteWins(remote); err != nil || len(dropped) != 0 {
		t.Fatalf("b pull = %v %v", dropped, err)
	}
	theirs := commit(b, "a.yaml", "theirs")
	if err := b.PushTo(remote); err != nil {
		t.Fatal(err)
	}
	ours := commit(a, "a.yaml", "ours")
	if err := a.PushTo(remote); !errors.Is(err, ErrRejected) {
		t.Errorf("divergent push = %v", err)
	}
	if st, err := a.Status(remote); err != nil || len(st.Ahead) != 1 || st.Ahead[0] != ours || st.Behind != 1 {
		t.Errorf("divergent status = %+v %v", st, err)
	}

	// Pull: the remote wins; ours is dropped and reported.
	dropped, err := a.PullRemoteWins(remote)
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
	if err := a.PushTo(remote); err != nil {
		t.Errorf("push after pull = %v", err)
	}

	// An unreachable remote is an error for every operation.
	missing := Remote{URL: filepath.Join(t.TempDir(), "missing.git")}
	if _, err := a.Status(missing); err == nil {
		t.Error("status of a missing remote succeeded")
	}
	if err := a.PushTo(missing); err == nil {
		t.Error("push to a missing remote succeeded")
	}
	if _, err := a.PullRemoteWins(missing); err == nil {
		t.Error("pull from a missing remote succeeded")
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
		if _, err := s.state(); err == nil {
			t.Errorf("%s: no error", ref)
		}
	}
}
