package state

import (
	"context"
	"database/sql"
	"errors"
	"sort"
)

// Flow definition errors.
var (
	ErrFlowNotFound = errors.New("state: flow not found")
	ErrFlowExists   = errors.New("state: flow already exists")
)

// FlowDefinition is a stored flow definition document (D-12). The store
// treats Document as opaque bytes; the owner of the flow model encodes it.
// The SQL and in-memory stores implement CreateFlow, GetFlow, ListFlows
// (ordered by ID), and DeleteFlow.
type FlowDefinition struct {
	ID       string
	Document []byte
}

func flowsMigration() Migration {
	return Migration{
		Version: 2,
		Name:    "flow-definitions",
		Apply: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS flows (
				id TEXT PRIMARY KEY,
				document TEXT NOT NULL
			)`)
			return err
		},
	}
}

// CreateFlow inserts f, returning ErrFlowExists if f.ID is taken.
func (s *sqlStore) CreateFlow(ctx context.Context, f FlowDefinition) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO flows (id, document) VALUES (?, ?) ON CONFLICT (id) DO NOTHING`, f.ID, string(f.Document))
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrFlowExists
	}
	return nil
}

func (s *sqlStore) GetFlow(ctx context.Context, id string) (FlowDefinition, error) {
	var doc string
	err := s.db.QueryRowContext(ctx, `SELECT document FROM flows WHERE id = ?`, id).Scan(&doc)
	if errors.Is(err, sql.ErrNoRows) {
		return FlowDefinition{}, ErrFlowNotFound
	}
	if err != nil {
		return FlowDefinition{}, err
	}
	return FlowDefinition{ID: id, Document: []byte(doc)}, nil
}

func (s *sqlStore) ListFlows(ctx context.Context) ([]FlowDefinition, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, document FROM flows ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []FlowDefinition{}
	for rows.Next() {
		var f FlowDefinition
		var doc string
		if err := rows.Scan(&f.ID, &doc); err != nil {
			return nil, err
		}
		f.Document = []byte(doc)
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *sqlStore) DeleteFlow(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM flows WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrFlowNotFound
	}
	return nil
}

// CreateFlow inserts f, returning ErrFlowExists if f.ID is taken.
func (s *MemStore) CreateFlow(_ context.Context, f FlowDefinition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.flows[f.ID]; ok {
		return ErrFlowExists
	}
	f.Document = append([]byte(nil), f.Document...)
	s.flows[f.ID] = f
	return nil
}

func (s *MemStore) GetFlow(_ context.Context, id string) (FlowDefinition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.flows[id]
	if !ok {
		return FlowDefinition{}, ErrFlowNotFound
	}
	f.Document = append([]byte(nil), f.Document...)
	return f, nil
}

func (s *MemStore) ListFlows(_ context.Context) ([]FlowDefinition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]FlowDefinition, 0, len(s.flows))
	for _, f := range s.flows {
		f.Document = append([]byte(nil), f.Document...)
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *MemStore) DeleteFlow(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.flows[id]; !ok {
		return ErrFlowNotFound
	}
	delete(s.flows, id)
	return nil
}
