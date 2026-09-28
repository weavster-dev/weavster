package state

import (
	"context"
	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx database/sql driver
)

// OpenPostgres opens a Postgres-backed Store (the production durable backend).
// The shared SQL core is written with "?" placeholders; the store rewrites
// them as $n for PostgreSQL (dialectDB; pgx does not). maxConns caps the
// pool's open connections (0 = unlimited).
func OpenPostgres(ctx context.Context, connString string, maxConns int) (Store, error) {
	db, err := sql.Open("pgx", connString)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns) // keep the pool: each new connection is a TLS handshake
	return openSQLStore(ctx, db, true)
}
