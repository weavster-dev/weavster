package secrets

import (
	"context"
	"fmt"
	"sync"
)

// Local is an in-memory local credential store (MVP adapter, arch §3.1).
type Local struct {
	mu sync.RWMutex
	m  map[string][]byte
}

// NewLocal returns an empty in-memory credential store.
func NewLocal() *Local {
	return &Local{m: make(map[string][]byte)}
}

// Set stores a copy of value under key.
func (l *Local) Set(key string, value []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cp := make([]byte, len(value))
	copy(cp, value)
	l.m[key] = cp
}

// Get returns a copy of the secret for key.
func (l *Local) Get(_ context.Context, key string) ([]byte, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	v, ok := l.m[key]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, key)
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, nil
}
