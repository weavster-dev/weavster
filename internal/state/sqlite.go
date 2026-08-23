package state

import (
	"context"
	"database/sql"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no CGo)
)

// OpenSQLite opens a SQLite-backed Store (local DX; no Postgres required,
// constraint #3). dsn may be ":memory:" or a file path.
func OpenSQLite(ctx context.Context, dsn string) (Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// Keep a single connection so an in-memory database is shared.
	db.SetMaxOpenConns(1)
	return openSQLStore(ctx, db)
}
