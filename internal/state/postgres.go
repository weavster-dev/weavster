package state

import (
	"context"
	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx database/sql driver
)

// OpenPostgres opens a Postgres-backed Store (the production durable backend).
// The pgx stdlib driver rewrites "?" placeholders to $n, so the shared SQL
// core is identical across backends. Not required for local DX or tests.
// maxConns caps the write pool's open connections (0 = unlimited).
func OpenPostgres(ctx context.Context, connString string, maxConns int) (Store, error) {
	db, err := sql.Open("pgx", connString)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(maxConns)
	return openSQLStore(ctx, db)
}
