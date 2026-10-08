package state

import (
	"context"
	"database/sql"
	"errors"
	"sort"
)

// UserDocument is a stored local-user record (durable local users, spec
// §10). The store treats Document as opaque bytes; the auth adapter encodes
// it. The SQL and in-memory stores implement PutUser (create or replace),
// ListUsers (ordered by username), and DeleteUser (a no-op for an unknown
// user). InsertUser adds a user only if the username is free.
type UserDocument struct {
	Username string
	Document []byte
}

func usersMigration() Migration {
	return Migration{
		Version: 3,
		Name:    "local-users",
		Apply: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS users (
				username TEXT PRIMARY KEY,
				document TEXT NOT NULL
			)`)
			return err
		},
	}
}

// ErrUserExists is returned by InsertUser when the username is taken.
var ErrUserExists = errors.New("state: user already exists")

func (s *sqlStore) InsertUser(ctx context.Context, u UserDocument) error {
	ctx = s.bind(ctx)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, document) VALUES (?, ?) ON CONFLICT (username) DO NOTHING`, u.Username, string(u.Document))
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrUserExists
	}
	return nil
}

func (s *sqlStore) PutUser(ctx context.Context, u UserDocument) error {
	ctx = s.bind(ctx)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (username, document) VALUES (?, ?)
		ON CONFLICT (username) DO UPDATE SET document = excluded.document`, u.Username, string(u.Document))
	return err
}

func (s *sqlStore) ListUsers(ctx context.Context) ([]UserDocument, error) {
	ctx = s.bind(ctx)
	rows, err := s.db.QueryContext(ctx, `SELECT username, document FROM users ORDER BY username /*C*/`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []UserDocument{}
	for rows.Next() {
		var u UserDocument
		var doc string
		if err := rows.Scan(&u.Username, &doc); err != nil {
			return nil, err
		}
		u.Document = []byte(doc)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *sqlStore) DeleteUser(ctx context.Context, username string) error {
	ctx = s.bind(ctx)
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE username = ?`, username)
	return err
}

func (s *MemStore) InsertUser(ctx context.Context, u UserDocument) error {
	s.mu.Lock()
	_, taken := s.users[u.Username]
	s.mu.Unlock()
	if taken {
		return ErrUserExists
	}
	return s.PutUser(ctx, u)
}

func (s *MemStore) PutUser(_ context.Context, u UserDocument) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u.Document = append([]byte(nil), u.Document...)
	s.users[u.Username] = u
	return nil
}

func (s *MemStore) ListUsers(_ context.Context) ([]UserDocument, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]UserDocument, 0, len(s.users))
	for _, u := range s.users {
		u.Document = append([]byte(nil), u.Document...)
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out, nil
}

func (s *MemStore) DeleteUser(_ context.Context, username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.users, username)
	return nil
}
