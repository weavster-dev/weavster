package state

import (
	"context"
	"sync"
)

// MemStore is an in-memory Store (passthrough/buffered backend; tests + local
// DX, constraint #3).
type MemStore struct {
	mu sync.RWMutex
	m  map[string]Message
}

// NewMemStore returns an empty in-memory store.
func NewMemStore() *MemStore {
	return &MemStore{m: make(map[string]Message)}
}

func (s *MemStore) Put(_ context.Context, m Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[m.ID] = m
	return nil
}

func (s *MemStore) Get(_ context.Context, id string) (Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.m[id]
	if !ok {
		return Message{}, ErrNotFound
	}
	return m, nil
}

func (s *MemStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
	return nil
}

func (s *MemStore) Search(_ context.Context, q Query) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var all []Message
	for _, m := range s.m {
		if matches(m, q) {
			all = append(all, m)
		}
	}
	sortMessages(all, q.Sort)

	if q.Offset > len(all) {
		all = nil
	} else {
		all = all[q.Offset:]
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit < len(all) {
		all = all[:limit]
	}
	return all, nil
}

func (s *MemStore) Close() error { return nil }

var _ Store = (*MemStore)(nil)
