package state

import "testing"

// TestMemStoreClose covers MemStore.Close, a no-op required to satisfy the
// Store interface's Close method but never previously invoked directly.
func TestMemStoreClose(t *testing.T) {
	s := NewMemStore()
	if err := s.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
}
