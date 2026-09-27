package state

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// MemStore is an in-memory Store (passthrough/buffered backend; tests + local
// DX, constraint #3).
type MemStore struct {
	mu      sync.RWMutex
	m       map[string]Message
	flows   map[string]FlowDefinition
	users   map[string]UserDocument
	items   map[string]map[string]json.RawMessage // kind -> name -> value
	lookups map[string]map[string]string          // group -> key -> value
}

// NewMemStore returns an empty in-memory store.
func NewMemStore() *MemStore {
	return &MemStore{m: make(map[string]Message), flows: make(map[string]FlowDefinition), users: make(map[string]UserDocument),
		lookups: map[string]map[string]string{}, items: make(map[string]map[string]json.RawMessage)}
}

func (s *MemStore) Put(_ context.Context, m Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Same timestamps as the SQL store: received once, updated on every put.
	now := time.Now()
	if m.ReceivedAt.IsZero() {
		m.ReceivedAt = now
		if old, ok := s.m[m.ID]; ok { // an update keeps the receive time, as the SQL upsert does
			m.ReceivedAt = old.ReceivedAt
		}
	}
	m.UpdatedAt = now
	s.m[m.ID] = cloneMessage(m)
	return nil
}

// cloneMessage copies a message's maps, so a caller changing its copy (as
// the pipeline does) never races a reader of the stored one.
func cloneMessage(m Message) Message {
	if m.Metadata != nil {
		md := make(map[string]string, len(m.Metadata))
		for k, v := range m.Metadata {
			md[k] = v
		}
		m.Metadata = md
	}
	if m.Attempts != nil {
		at := make(map[string]DestinationAttempt, len(m.Attempts))
		for k, v := range m.Attempts {
			at[k] = v
		}
		m.Attempts = at
	}
	return m
}

func (s *MemStore) Get(_ context.Context, id string) (Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.m[id]
	if !ok {
		return Message{}, ErrNotFound
	}
	return cloneMessage(m), nil
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
			all = append(all, cloneMessage(m))
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
