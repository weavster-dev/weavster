package state

import (
	"context"
	"database/sql"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/stdlib"
)

// isPostgres reports whether db is a PostgreSQL connection (the pgx
// driver); otherwise it is SQLite.
func isPostgres(db *sql.DB) bool {
	return db.Driver() == stdlib.GetDefaultDriver()
}

// rebind rewrites the store's "?" placeholders as PostgreSQL's $1, $2, …
// (pgx does not; SQLite takes "?" as written). The store's statements
// contain no "?" inside string literals.
func rebind(postgres bool, query string) string {
	if !postgres || !strings.Contains(query, "?") {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			b.WriteString("$" + strconv.Itoa(n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// dialectDB is the store's database: statements written with "?" run on
// SQLite and PostgreSQL alike.
type dialectDB struct {
	*sql.DB
	postgres bool
}

func newDialectDB(db *sql.DB) *dialectDB { return &dialectDB{DB: db, postgres: isPostgres(db)} }

func (d *dialectDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.DB.ExecContext(ctx, rebind(d.postgres, query), args...)
}

func (d *dialectDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.DB.QueryContext(ctx, rebind(d.postgres, query), args...)
}

func (d *dialectDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.DB.QueryRowContext(ctx, rebind(d.postgres, query), args...)
}

// BeginTx starts a transaction whose statements are rebound too.
func (d *dialectDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*dialectTx, error) {
	tx, err := d.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &dialectTx{Tx: tx, postgres: d.postgres}, nil
}

// dialectTx is a transaction of a dialectDB.
type dialectTx struct {
	*sql.Tx
	postgres bool
}

func (t *dialectTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.Tx.ExecContext(ctx, rebind(t.postgres, query), args...)
}

func (t *dialectTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.Tx.QueryContext(ctx, rebind(t.postgres, query), args...)
}

func (t *dialectTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.Tx.QueryRowContext(ctx, rebind(t.postgres, query), args...)
}

func (t *dialectTx) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return t.Tx.PrepareContext(ctx, rebind(t.postgres, query))
}
