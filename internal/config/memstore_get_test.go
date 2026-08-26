package config

import (
	"context"
	"testing"
)

// TestMemStoreGet covers MemStore.Get, which previously had 0% coverage.
// The apply/drift workflows depend on Get returning the stored value with
// ok=true, and (nil, false, nil) for a missing key rather than an error.
func TestMemStoreGet(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	if _, ok, err := store.Get(ctx, "missing"); err != nil || ok {
		t.Fatalf("Get(missing) = (_, %v, %v), want (_, false, nil)", ok, err)
	}

	if err := store.Put(ctx, "flow/a", []byte("v1")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	v, ok, err := store.Get(ctx, "flow/a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("Get(flow/a) ok = false, want true")
	}
	if string(v) != "v1" {
		t.Errorf("Get(flow/a) = %q, want %q", v, "v1")
	}
}
