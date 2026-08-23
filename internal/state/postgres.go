package state

import (
	"context"
	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx database/sql driver
)

// OpenPostgres opens a Postgres-backed Store (the production durable backend).
// The pgx stdlib driver rewrites "?" placeholders to $n, so the shared SQL
// core is identical across backends. Not required for local DX or tests.
func OpenPostgres(ctx context.Context, connString string) (Store, error) {
	db, err := sql.Open("pgx", connString)
	if err != nil {
		return nil, err
	}
	return openSQLStore(ctx, db)
}
