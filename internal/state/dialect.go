package state

import (
	"context"
	"database/sql"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/stdlib"
)

// isPostgres reports whether db is a PostgreSQL connection (the pgx
// driver); otherwise it is SQLite. Used where only a *sql.DB is at hand
// (Migrate); the store is told its dialect when it opens.
func isPostgres(db *sql.DB) bool {
	return db.Driver() == stdlib.GetDefaultDriver()
}

// byteOrder marks a text comparison or ORDER BY that must use byte order,
// as SQLite and the memory store do: a comment SQLite ignores, which
// becomes COLLATE "C" on PostgreSQL (whose default collation follows the
// database's locale, so pages would differ between backends).
const byteOrder = "/*C*/"

// rebind rewrites a statement written for SQLite for PostgreSQL: "?"
// placeholders become $1, $2, … (pgx does not do this), and byteOrder
// marks become COLLATE "C". The store's statements contain no "?" inside
// string literals.
func rebind(postgres bool, query string) string {
	if !postgres {
		return query
	}
	query = strings.ReplaceAll(query, byteOrder, `COLLATE "C"`)
	if !strings.Contains(query, "?") {
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

// dialectDB is the store's database: statements written for SQLite run on
// PostgreSQL too. It offers only the methods that rebind, so none can be
// used by mistake without it.
type dialectDB struct {
	db       *sql.DB
	postgres bool
}

func (d *dialectDB) Close() error { return d.db.Close() }

func (d *dialectDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.db.ExecContext(ctx, rebind(d.postgres, query), args...)
}

func (d *dialectDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.db.QueryContext(ctx, rebind(d.postgres, query), args...)
}

func (d *dialectDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.db.QueryRowContext(ctx, rebind(d.postgres, query), args...)
}

// BeginTx starts a transaction whose statements are rebound too.
func (d *dialectDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*dialectTx, error) {
	tx, err := d.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &dialectTx{tx: tx, postgres: d.postgres}, nil
}

// dialectTx is a transaction of a dialectDB, with only rebinding methods.
type dialectTx struct {
	tx       *sql.Tx
	postgres bool
}

func (t *dialectTx) Commit() error   { return t.tx.Commit() }
func (t *dialectTx) Rollback() error { return t.tx.Rollback() }

func (t *dialectTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, rebind(t.postgres, query), args...)
}

func (t *dialectTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, rebind(t.postgres, query), args...)
}

func (t *dialectTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, rebind(t.postgres, query), args...)
}

func (t *dialectTx) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return t.tx.PrepareContext(ctx, rebind(t.postgres, query))
}

// textValue is s as it can be stored in a text column of either database:
// PostgreSQL refuses NUL bytes and invalid UTF-8, so NULs are dropped and
// invalid bytes become U+FFFD, on every backend alike.
func textValue(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	return strings.ToValidUTF8(s, "�")
}
