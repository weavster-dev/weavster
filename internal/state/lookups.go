package state

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
)

// ErrLookupNotFound is returned for a missing lookup key or group.
var ErrLookupNotFound = errors.New("state: lookup not found")

// Lookups are groups of string key → string value (spec §5 dynamic
// lookups). The SQL and in-memory stores implement LookupGroups,
// LookupEntries, LookupGet, LookupPut, LookupDelete, and LookupDeleteGroup.

func lookupsMigration() Migration {
	return Migration{
		Version: 7,
		Name:    "lookups",
		Apply: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS lookups (
				grp TEXT NOT NULL,
				key TEXT NOT NULL,
				value TEXT NOT NULL,
				PRIMARY KEY (grp, key)
			)`)
			return err
		},
	}
}

// LookupGroups returns every group with its number of entries.
func (s *sqlStore) LookupGroups(ctx context.Context) (map[string]int, error) {
	ctx = s.bind(ctx)
	rows, err := s.db.QueryContext(ctx, `SELECT grp, COUNT(*) FROM lookups GROUP BY grp`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int{}
	for rows.Next() {
		var g string
		var n int
		if err := rows.Scan(&g, &n); err != nil {
			return nil, err
		}
		out[g] = n
	}
	return out, rows.Err()
}

// LookupEntries returns a group's entries whose key starts with prefix,
// the first limit in key order (0 = all).
func (s *sqlStore) LookupEntries(ctx context.Context, group, prefix string, limit int) (map[string]string, error) {
	ctx = s.bind(ctx)
	q := `SELECT key, value FROM lookups WHERE grp = ? AND substr(key, 1, ?) = ? ORDER BY key`
	args := []any{group, len([]rune(prefix)), prefix}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// LookupGet returns the values of the keys that exist.
func (s *sqlStore) LookupGet(ctx context.Context, group string, keys []string) (map[string]string, error) {
	ctx = s.bind(ctx)
	out := map[string]string{}
	for _, k := range keys {
		var v string
		err := s.db.QueryRowContext(ctx, `SELECT value FROM lookups WHERE grp = ? AND key = ?`, group, k).Scan(&v)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, nil
}

// LookupPut creates or replaces entries in one transaction; with replace
// the group holds exactly entries afterwards.
func (s *sqlStore) LookupPut(ctx context.Context, group string, entries map[string]string, replace bool) error {
	ctx = s.bind(ctx)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if replace {
		if _, err := tx.ExecContext(ctx, `DELETE FROM lookups WHERE grp = ?`, group); err != nil {
			return err
		}
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO lookups (grp, key, value) VALUES (?, ?, ?)
		ON CONFLICT (grp, key) DO UPDATE SET value = excluded.value`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for k, v := range entries {
		if _, err := stmt.ExecContext(ctx, group, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LookupDelete removes one entry (ErrLookupNotFound).
func (s *sqlStore) LookupDelete(ctx context.Context, group, key string) error {
	return s.lookupDeleteWhere(ctx, `DELETE FROM lookups WHERE grp = ? AND key = ?`, group, key)
}

// LookupDeleteGroup removes every entry of a group (ErrLookupNotFound when
// it has none).
func (s *sqlStore) LookupDeleteGroup(ctx context.Context, group string) error {
	return s.lookupDeleteWhere(ctx, `DELETE FROM lookups WHERE grp = ?`, group)
}

func (s *sqlStore) lookupDeleteWhere(ctx context.Context, q string, args ...any) error {
	res, err := s.db.ExecContext(s.bind(ctx), q, args...)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrLookupNotFound
	}
	return nil
}

// LookupGroups returns every group with its number of entries.
func (s *MemStore) LookupGroups(context.Context) (map[string]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]int{}
	for g, entries := range s.lookups {
		out[g] = len(entries)
	}
	return out, nil
}

// LookupEntries returns a group's entries whose key starts with prefix,
// the first limit in key order (0 = all).
func (s *MemStore) LookupEntries(_ context.Context, group, prefix string, limit int) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]string, 0, len(s.lookups[group]))
	for k := range s.lookups[group] {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if limit > 0 && len(keys) > limit {
		keys = keys[:limit]
	}
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		out[k] = s.lookups[group][k]
	}
	return out, nil
}

// LookupGet returns the values of the keys that exist.
func (s *MemStore) LookupGet(_ context.Context, group string, keys []string) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]string{}
	for _, k := range keys {
		if v, ok := s.lookups[group][k]; ok {
			out[k] = v
		}
	}
	return out, nil
}

// LookupPut creates or replaces entries; with replace the group holds
// exactly entries afterwards.
func (s *MemStore) LookupPut(_ context.Context, group string, entries map[string]string, replace bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if replace || s.lookups[group] == nil {
		s.lookups[group] = map[string]string{}
	}
	for k, v := range entries {
		s.lookups[group][k] = v
	}
	if len(s.lookups[group]) == 0 {
		delete(s.lookups, group) // a group exists while it has entries
	}
	return nil
}

// LookupDelete removes one entry (ErrLookupNotFound).
func (s *MemStore) LookupDelete(_ context.Context, group, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.lookups[group][key]; !ok {
		return ErrLookupNotFound
	}
	delete(s.lookups[group], key)
	if len(s.lookups[group]) == 0 {
		delete(s.lookups, group)
	}
	return nil
}

// LookupDeleteGroup removes every entry of a group (ErrLookupNotFound when
// it has none).
func (s *MemStore) LookupDeleteGroup(_ context.Context, group string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.lookups[group]) == 0 {
		return ErrLookupNotFound
	}
	delete(s.lookups, group)
	return nil
}
