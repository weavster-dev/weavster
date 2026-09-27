package main

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/state"
)

// TestSnippetsAdapterCorruptData: stored values that no longer decode are
// reported as errors, not returned as empty snippets.
func TestSnippetsAdapterCorruptData(t *testing.T) {
	ctx := context.Background()
	mem := state.NewMemStore()
	_ = mem.PutItem(ctx, snippetKind, "bad", json.RawMessage(`"not an object"`))
	a := snippetsAdapter{repo: mem, mu: &sync.Mutex{}}
	for name, call := range map[string]func() error{
		"list": func() error { _, err := a.ListSnippets(ctx); return err },
		"get":  func() error { _, err := a.GetSnippet(ctx, "bad"); return err },
	} {
		if err := call(); err == nil {
			t.Errorf("%s: error = nil, want a decode error", name)
		}
	}
	if err := a.SaveLibraries(ctx, []gateway.SnippetLibrary{{Name: "l"}}, true); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveLibraries(ctx, []gateway.SnippetLibrary{{Name: "l"}}, true); err != gateway.ErrLibraryExists {
		t.Errorf("create existing = %v, want ErrLibraryExists", err)
	}
	if err := a.DeleteLibrary(ctx, "l"); err == nil {
		t.Error("delete library with a corrupt snippet stored: error = nil, want a decode error")
	}
	if err := a.DeleteLibrary(ctx, "missing"); err != gateway.ErrLibraryNotFound {
		t.Errorf("delete missing library = %v, want ErrLibraryNotFound", err)
	}
}
