// Package sqldialect lets one SQL statement, written for SQLite, run on
// PostgreSQL too.
package sqldialect

import (
	"database/sql"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/stdlib"
)

// ByteOrder marks a text comparison or ORDER BY that must use byte order,
// as SQLite does: a comment SQLite ignores, which becomes COLLATE "C" on
// PostgreSQL (whose default collation follows the database's locale).
const ByteOrder = "/*C*/"

// IsPostgres reports whether db is a PostgreSQL connection (the pgx
// driver); otherwise it is SQLite.
func IsPostgres(db *sql.DB) bool {
	return db.Driver() == stdlib.GetDefaultDriver()
}

// Rebind rewrites a statement written for SQLite for PostgreSQL: "?"
// placeholders become $1, $2, … (pgx does not do this), and ByteOrder
// marks become COLLATE "C". Statements must not hold "?" inside string
// literals.
func Rebind(postgres bool, query string) string {
	if !postgres {
		return query
	}
	query = strings.ReplaceAll(query, ByteOrder, `COLLATE "C"`)
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
