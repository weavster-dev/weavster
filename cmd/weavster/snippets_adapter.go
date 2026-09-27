package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/state"
)

// Snippets and libraries are stored as config items of these kinds.
const (
	snippetKind = "snippets"
	libraryKind = "snippet-libraries"
)

// snippetsAdapter serves gateway.SnippetStore from the item store. mu makes
// the existence checks and the writes one step, so a snippet never names a
// deleted library and create never overwrites. The lock is per process: one
// server per database (D-41).
type snippetsAdapter struct {
	repo itemRepository
	mu   *sync.Mutex
}

// listDocs decodes every item of kind into T.
func listDocs[T any](ctx context.Context, repo itemRepository, kind string) ([]T, error) {
	items, err := repo.ListItems(ctx, kind)
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(items))
	for _, raw := range items {
		var v T
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// getDoc decodes one item of kind into T, returning notFound when missing.
func getDoc[T any](ctx context.Context, repo itemRepository, kind, name string, notFound error) (T, error) {
	var v T
	raw, err := repo.GetItem(ctx, kind, name)
	if errors.Is(err, state.ErrItemNotFound) {
		return v, notFound
	}
	if err != nil {
		return v, err
	}
	return v, json.Unmarshal(raw, &v)
}

// saveDocs writes docs of kind in one transaction; with create, none may
// exist yet (exists).
func saveDocs[T any](ctx context.Context, repo itemRepository, kind string, docs []T, name func(T) string, create bool, exists error) error {
	items := make(map[string]json.RawMessage, len(docs))
	for _, d := range docs {
		if create {
			if _, err := repo.GetItem(ctx, kind, name(d)); err == nil {
				return exists
			} else if !errors.Is(err, state.ErrItemNotFound) {
				return err
			}
		}
		raw, err := json.Marshal(d)
		if err != nil {
			return err
		}
		items[name(d)] = raw
	}
	return repo.PutItems(ctx, kind, items)
}

func deleteDoc(ctx context.Context, repo itemRepository, kind, name string, notFound error) error {
	err := repo.DeleteItem(ctx, kind, name)
	if errors.Is(err, state.ErrItemNotFound) {
		return notFound
	}
	return err
}

func (a snippetsAdapter) ListSnippets(ctx context.Context) ([]gateway.Snippet, error) {
	return listDocs[gateway.Snippet](ctx, a.repo, snippetKind)
}

func (a snippetsAdapter) GetSnippet(ctx context.Context, name string) (gateway.Snippet, error) {
	return getDoc[gateway.Snippet](ctx, a.repo, snippetKind, name, gateway.ErrSnippetNotFound)
}

func (a snippetsAdapter) SaveSnippets(ctx context.Context, list []gateway.Snippet, create bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	checked := map[string]bool{"": true}
	for _, sn := range list {
		if checked[sn.Library] {
			continue
		}
		if _, err := a.GetLibrary(ctx, sn.Library); err != nil {
			return err
		}
		checked[sn.Library] = true
	}
	return saveDocs(ctx, a.repo, snippetKind, list, func(s gateway.Snippet) string { return s.Name }, create, gateway.ErrSnippetExists)
}

func (a snippetsAdapter) DeleteSnippet(ctx context.Context, name string) error {
	return deleteDoc(ctx, a.repo, snippetKind, name, gateway.ErrSnippetNotFound)
}

func (a snippetsAdapter) ListLibraries(ctx context.Context) ([]gateway.SnippetLibrary, error) {
	return listDocs[gateway.SnippetLibrary](ctx, a.repo, libraryKind)
}

func (a snippetsAdapter) GetLibrary(ctx context.Context, name string) (gateway.SnippetLibrary, error) {
	return getDoc[gateway.SnippetLibrary](ctx, a.repo, libraryKind, name, gateway.ErrLibraryNotFound)
}

func (a snippetsAdapter) SaveLibraries(ctx context.Context, list []gateway.SnippetLibrary, create bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return saveDocs(ctx, a.repo, libraryKind, list, func(l gateway.SnippetLibrary) string { return l.Name }, create, gateway.ErrLibraryExists)
}

func (a snippetsAdapter) DeleteLibrary(ctx context.Context, name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.GetLibrary(ctx, name); err != nil {
		return err
	}
	// Decode only the library of each snippet, not its code.
	snippets, err := listDocs[struct {
		Library string `json:"library"`
	}](ctx, a.repo, snippetKind)
	if err != nil {
		return err
	}
	for _, sn := range snippets {
		if sn.Library == name {
			return gateway.ErrLibraryInUse
		}
	}
	return deleteDoc(ctx, a.repo, libraryKind, name, gateway.ErrLibraryNotFound)
}
