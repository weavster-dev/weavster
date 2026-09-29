package gitstore

import (
	"errors"
	"io"
	"testing"

	"github.com/go-git/go-billy/v5/memfs"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"
)

var errRevisionRead = errors.New("injected revision storage failure")

type revisionReadStorage struct {
	*memory.Storage
	failType       plumbing.ObjectType
	failBlobReader bool
}

func (s *revisionReadStorage) EncodedObject(kind plumbing.ObjectType, hash plumbing.Hash) (plumbing.EncodedObject, error) {
	if kind == s.failType {
		return nil, errRevisionRead
	}
	obj, err := s.Storage.EncodedObject(kind, hash)
	if err == nil && kind == plumbing.BlobObject && s.failBlobReader {
		return revisionReadObject{EncodedObject: obj}, nil
	}
	return obj, err
}

type revisionReadObject struct{ plumbing.EncodedObject }

func (revisionReadObject) Reader() (io.ReadCloser, error) {
	return nil, errRevisionRead
}

func newRevisionReadStore(t *testing.T) (*Store, *revisionReadStorage, string) {
	t.Helper()
	storage := &revisionReadStorage{Storage: memory.NewStorage()}
	repo, err := git.Init(storage, memfs.New())
	if err != nil {
		t.Fatal(err)
	}
	s, err := openFrom(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WriteFile("flow.yaml", []byte("name: admit")); err != nil {
		t.Fatal(err)
	}
	rev, err := s.Commit("add flow", Author{Name: "tester", Email: "t@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	return s, storage, rev
}

func TestLogAndHistoryStorageErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		read func(*Store) ([]Revision, error)
	}{
		{"Log", (*Store).Log},
		{"History", func(s *Store) ([]Revision, error) { return s.History("flow.yaml") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, storage, rev := newRevisionReadStore(t)
			got, err := tc.read(s)
			if err != nil || len(got) != 1 || got[0].Hash != rev {
				t.Fatalf("baseline revisions = %v, error = %v, want commit %s", got, err, rev)
			}
			storage.failType = plumbing.CommitObject
			got, err = tc.read(s)
			if !errors.Is(err, errRevisionRead) {
				t.Fatalf("error = %v, want %v", err, errRevisionRead)
			}
			if got != nil {
				t.Errorf("revisions = %v, want nil on storage failure", got)
			}
		})
	}
}

func TestContentAtRevisionStorageErrors(t *testing.T) {
	for _, tc := range []struct {
		name           string
		failType       plumbing.ObjectType
		failBlobReader bool
	}{
		{"commit lookup", plumbing.CommitObject, false},
		{"tree lookup", plumbing.TreeObject, false},
		{"blob lookup", plumbing.BlobObject, false},
		{"blob reader", plumbing.InvalidObject, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, storage, rev := newRevisionReadStore(t)
			got, err := s.ContentAtRevision("flow.yaml", rev)
			if err != nil || string(got) != "name: admit" {
				t.Fatalf("baseline content = %q, error = %v", got, err)
			}
			storage.failType = tc.failType
			storage.failBlobReader = tc.failBlobReader
			got, err = s.ContentAtRevision("flow.yaml", rev)
			if !errors.Is(err, errRevisionRead) {
				t.Fatalf("error = %v, want %v", err, errRevisionRead)
			}
			if got != nil {
				t.Errorf("content = %q, want nil on storage failure", got)
			}
		})
	}
}
