package state

import (
	"context"
	"database/sql"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no CGo)
)

// OpenSQLite opens a SQLite-backed Store for tests that need a SQL store
// without PostgreSQL (constraint #3); the server does not offer it (D-55).
// dsn may be ":memory:" or a file path.
func OpenSQLite(ctx context.Context, dsn string) (Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// Keep a single connection so an in-memory database is shared.
	db.SetMaxOpenConns(1)
	s, err := openSQLStore(context.WithoutCancel(ctx), db, false)
	if err != nil {
		return nil, err
	}
	s.uncancelable = true
	return s, nil
}
