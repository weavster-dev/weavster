package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// ErrItemNotFound is returned for a missing named item.
var ErrItemNotFound = errors.New("state: item not found")

// Config items are named JSON values grouped by kind (the config map, global
// scripts, settings). The SQL and in-memory stores implement ListItems,
// GetItem, PutItem, PutItems, DeleteItem, and ReplaceItems.

func itemsMigration() Migration {
	return Migration{
		Version: 6,
		Name:    "config-items",
		Apply: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS config_items (
				kind TEXT NOT NULL,
				name TEXT NOT NULL,
				value TEXT NOT NULL,
				PRIMARY KEY (kind, name)
			)`)
			return err
		},
	}
}

// ListItems returns every item of kind.
func (s *sqlStore) ListItems(ctx context.Context, kind string) (map[string]json.RawMessage, error) {
	ctx = s.bind(ctx)
	rows, err := s.db.QueryContext(ctx, `SELECT name, value FROM config_items WHERE kind = ?`, kind)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			return nil, err
		}
		out[name] = json.RawMessage(value)
	}
	return out, rows.Err()
}

// GetItem returns one item (ErrItemNotFound).
func (s *sqlStore) GetItem(ctx context.Context, kind, name string) (json.RawMessage, error) {
	ctx = s.bind(ctx)
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM config_items WHERE kind = ? AND name = ?`, kind, name).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrItemNotFound
	}
	return json.RawMessage(value), err
}

// PutItem creates or replaces an item.
func (s *sqlStore) PutItem(ctx context.Context, kind, name string, value json.RawMessage) error {
	ctx = s.bind(ctx)
	_, err := s.db.ExecContext(ctx, `INSERT INTO config_items (kind, name, value) VALUES (?, ?, ?)
		ON CONFLICT (kind, name) DO UPDATE SET value = excluded.value`, kind, name, string(value))
	return err
}

// DeleteItem removes an item (ErrItemNotFound).
func (s *sqlStore) DeleteItem(ctx context.Context, kind, name string) error {
	ctx = s.bind(ctx)
	res, err := s.db.ExecContext(ctx, `DELETE FROM config_items WHERE kind = ? AND name = ?`, kind, name)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrItemNotFound
	}
	return nil
}

// ReplaceItems makes items the whole set of kind, in one transaction.
func (s *sqlStore) ReplaceItems(ctx context.Context, kind string, items map[string]json.RawMessage) error {
	ctx = s.bind(ctx)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM config_items WHERE kind = ?`, kind); err != nil {
		return err
	}
	// Upsert: a concurrent PutItem may add a row between the DELETE and
	// these inserts under READ COMMITTED.
	if err := upsertItems(ctx, tx, kind, items); err != nil {
		return err
	}
	return tx.Commit()
}

// upsertItems creates or replaces items of kind inside tx.
func upsertItems(ctx context.Context, tx *dialectTx, kind string, items map[string]json.RawMessage) error {
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO config_items (kind, name, value) VALUES (?, ?, ?)
		ON CONFLICT (kind, name) DO UPDATE SET value = excluded.value`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for name, value := range items {
		if _, err := stmt.ExecContext(ctx, kind, name, string(value)); err != nil {
			return err
		}
	}
	return nil
}

// PutItems creates or replaces several items of kind, in one transaction.
func (s *sqlStore) PutItems(ctx context.Context, kind string, items map[string]json.RawMessage) error {
	ctx = s.bind(ctx)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := upsertItems(ctx, tx, kind, items); err != nil {
		return err
	}
	return tx.Commit()
}

// ListItems returns every item of kind.
func (s *MemStore) ListItems(_ context.Context, kind string) (map[string]json.RawMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]json.RawMessage{}
	for name, v := range s.items[kind] {
		out[name] = append(json.RawMessage(nil), v...)
	}
	return out, nil
}

// GetItem returns one item (ErrItemNotFound).
func (s *MemStore) GetItem(_ context.Context, kind, name string) (json.RawMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.items[kind][name]
	if !ok {
		return nil, ErrItemNotFound
	}
	return append(json.RawMessage(nil), v...), nil
}

// PutItem creates or replaces an item.
func (s *MemStore) PutItem(_ context.Context, kind, name string, value json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items[kind] == nil {
		s.items[kind] = map[string]json.RawMessage{}
	}
	s.items[kind][name] = append(json.RawMessage(nil), value...)
	return nil
}

// DeleteItem removes an item (ErrItemNotFound).
func (s *MemStore) DeleteItem(_ context.Context, kind, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[kind][name]; !ok {
		return ErrItemNotFound
	}
	delete(s.items[kind], name)
	return nil
}

// ReplaceItems makes items the whole set of kind.
func (s *MemStore) ReplaceItems(_ context.Context, kind string, items map[string]json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	set := make(map[string]json.RawMessage, len(items))
	for name, v := range items {
		set[name] = append(json.RawMessage(nil), v...)
	}
	s.items[kind] = set
	return nil
}

// PutItems creates or replaces several items of kind.
func (s *MemStore) PutItems(_ context.Context, kind string, items map[string]json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items[kind] == nil {
		s.items[kind] = map[string]json.RawMessage{}
	}
	for name, v := range items {
		s.items[kind][name] = append(json.RawMessage(nil), v...)
	}
	return nil
}
