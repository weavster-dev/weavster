package state

import (
	"context"
	"testing"
)

// TestMemStoreClose covers MemStore.Close, which had no direct test even
// though Store implementations are expected to be safely closeable (e.g. on
// server shutdown) and satisfy io.Closer-style semantics.
func TestMemStoreClose(t *testing.T) {
	s := NewMemStore()
	if err := s.Put(context.Background(), Message{ID: "1"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("Close() unexpected error: %v", err)
	}
	// Close should be a no-op and not affect subsequent operations.
	if _, err := s.Get(context.Background(), "1"); err != nil {
		t.Errorf("Get after Close: %v", err)
	}
}
