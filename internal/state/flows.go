package state

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"
)

// ErrFlowNotFound is returned when a flow id does not exist.
var ErrFlowNotFound = errors.New("state: flow not found")

// FlowDefinition is a stored flow definition document (D-12). The Store
// treats Document as opaque bytes; the owner of the flow model encodes it.
type FlowDefinition struct {
	ID        string
	Document  []byte
	UpdatedAt time.Time
}

// FlowStore is the port for durable flow definitions (D-12).
type FlowStore interface {
	// PutFlow creates or replaces the definition with f.ID.
	PutFlow(ctx context.Context, f FlowDefinition) error
	GetFlow(ctx context.Context, id string) (FlowDefinition, error)
	// ListFlows returns every definition ordered by ID.
	ListFlows(ctx context.Context) ([]FlowDefinition, error)
	DeleteFlow(ctx context.Context, id string) error
}

func flowsMigration() Migration {
	return Migration{
		Version: 2,
		Name:    "flow-definitions",
		Apply: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS flows (
				id TEXT PRIMARY KEY,
				document TEXT NOT NULL,
				updated_at INTEGER NOT NULL
			)`)
			return err
		},
	}
}

func (s *sqlStore) PutFlow(ctx context.Context, f FlowDefinition) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO flows (id, document, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET document = excluded.document, updated_at = excluded.updated_at`,
		f.ID, string(f.Document), time.Now().UnixMilli())
	return err
}

func (s *sqlStore) GetFlow(ctx context.Context, id string) (FlowDefinition, error) {
	var (
		doc string
		ms  int64
	)
	err := s.db.QueryRowContext(ctx, `SELECT document, updated_at FROM flows WHERE id = ?`, id).Scan(&doc, &ms)
	if errors.Is(err, sql.ErrNoRows) {
		return FlowDefinition{}, ErrFlowNotFound
	}
	if err != nil {
		return FlowDefinition{}, err
	}
	return FlowDefinition{ID: id, Document: []byte(doc), UpdatedAt: time.UnixMilli(ms)}, nil
}

func (s *sqlStore) ListFlows(ctx context.Context) ([]FlowDefinition, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, document, updated_at FROM flows ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []FlowDefinition{}
	for rows.Next() {
		var (
			f   FlowDefinition
			doc string
			ms  int64
		)
		if err := rows.Scan(&f.ID, &doc, &ms); err != nil {
			return nil, err
		}
		f.Document, f.UpdatedAt = []byte(doc), time.UnixMilli(ms)
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *sqlStore) DeleteFlow(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM flows WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrFlowNotFound
	}
	return nil
}

func (s *MemStore) PutFlow(_ context.Context, f FlowDefinition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f.Document = append([]byte(nil), f.Document...)
	f.UpdatedAt = time.Now()
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
