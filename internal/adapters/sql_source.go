package adapters

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// selectStatement is a query a database source may run: one SELECT (or
// WITH … SELECT) statement, a trailing semicolon allowed (#107 D-76).
var selectStatement = regexp.MustCompile(`(?is)^\s*(select|with)\b[^;]*;?\s*$`)

// writeKeyword finds a statement that changes data or schema, also inside
// a WITH (WITH x AS (DELETE …) SELECT …, WITH … UPDATE …).
var writeKeyword = regexp.MustCompile(`(?i)\b(insert|update|delete|merge|create|drop|alter|truncate|grant|revoke)\b`)

// ValidSelect reports whether query is a single SELECT or WITH … SELECT
// statement that names no statement changing data (the read-only
// transaction also refuses writes at run time).
func ValidSelect(query string) bool {
	return selectStatement.MatchString(query) && !writeKeyword.MatchString(query)
}

// MaxSQLBatchBytes bounds the rows one poll holds in memory (their JSON);
// the rest come with the next polls.
const MaxSQLBatchBytes = 64 << 20

// SQLRow is one row a database source read: its id as text and its
// columns as a JSON object (Body).
type SQLRow struct {
	ID   string
	Body []byte
}

// SQLQueryOptions describe a database source's read: Query (checked with
// ValidSelect) run read-only within Timeout, at most MaxRows rows, and
// IDColumn, the column identifying each row.
type SQLQueryOptions struct {
	Dialect  string
	Query    string
	IDColumn string
	MaxRows  int
	Timeout  time.Duration
}

// QuerySQL runs a database source's query read-only (a READ ONLY
// transaction on PostgreSQL, PRAGMA query_only on SQLite, which ignores
// read-only transactions) with LIMIT MaxRows, and returns the rows, as
// many as fit in MaxSQLBatchBytes (at least one), and whether it stopped
// at a limit (more rows may be waiting). Errors never quote values.
func QuerySQL(ctx context.Context, db *sql.DB, o SQLQueryOptions) ([]SQLRow, bool, error) {
	if o.MaxRows <= 0 {
		o.MaxRows = 100
	}
	rows, err := querySQL(ctx, db, o)
	if err != nil {
		return nil, false, err
	}
	return rows, len(rows) == o.MaxRows || batchBytes(rows) >= MaxSQLBatchBytes, nil
}

// batchBytes is the size of rows' JSON.
func batchBytes(rows []SQLRow) int {
	n := 0
	for _, r := range rows {
		n += len(r.Body)
	}
	return n
}

// querySQL is QuerySQL without the "more" answer.
func querySQL(ctx context.Context, db *sql.DB, o SQLQueryOptions) ([]SQLRow, error) {
	if !ValidSelect(o.Query) {
		return nil, errors.New("database: query must be one SELECT or WITH statement")
	}
	if o.Timeout <= 0 {
		o.Timeout = DBSinkTimeout
	}
	if o.MaxRows <= 0 {
		o.MaxRows = 100
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	fail := func(err error) error { return dbError(ctx, o.Dialect, o.Timeout, err) }
	// The database stops after MaxRows rows, instead of sending the whole
	// result for the driver to drain.
	query := "SELECT * FROM (" + strings.TrimRight(strings.TrimSpace(o.Query), "; \t\r\n") + ") AS weavster_rows LIMIT " + strconv.Itoa(o.MaxRows)
	var rows *sql.Rows
	if o.Dialect == DialectSQLite {
		conn, err := db.Conn(ctx)
		if err != nil {
			return nil, fail(err)
		}
		defer func() { _ = conn.Close() }()
		if _, err := conn.ExecContext(ctx, "PRAGMA query_only = ON"); err != nil {
			return nil, fail(err)
		}
		// Back to writable before the connection returns to the pool; one
		// that stays read-only is discarded instead.
		defer func() {
			if _, err := conn.ExecContext(context.WithoutCancel(ctx), "PRAGMA query_only = OFF"); err != nil {
				_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			}
		}()
		if rows, err = conn.QueryContext(ctx, query); err != nil {
			return nil, fail(err)
		}
	} else {
		tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return nil, fail(err)
		}
		defer func() { _ = tx.Rollback() }()
		if rows, err = tx.QueryContext(ctx, query); err != nil {
			return nil, fail(err)
		}
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		return nil, fail(err)
	}
	idAt := -1
	for i, c := range cols {
		if c == o.IDColumn {
			idAt = i
		}
	}
	if idAt < 0 {
		return nil, errors.New("database: the query's result has no idColumn " + strconv.Quote(o.IDColumn))
	}
	var out []SQLRow
	size := 0
	for size < MaxSQLBatchBytes && rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fail(err)
		}
		values := make(map[string]any, len(cols))
		for i, c := range cols {
			values[c] = jsonValue(vals[i])
		}
		if vals[idAt] == nil {
			return nil, errors.New("database: a row's idColumn is NULL")
		}
		body, _ := json.Marshal(values) // column values are JSON values
		size += len(body)
		out = append(out, SQLRow{ID: idText(values[cols[idAt]]), Body: body})
	}
	if err := rows.Err(); err != nil {
		return nil, fail(err)
	}
	return out, nil
}

// jsonValue is a column value as JSON: numbers (non-finite floats as
// text), booleans, text, times in RFC 3339, bytes as text when UTF-8 and
// base64 otherwise, NULL as null.
func jsonValue(v any) any {
	switch t := v.(type) {
	case float64:
		if math.IsInf(t, 0) || math.IsNaN(t) {
			return strconv.FormatFloat(t, 'g', -1, 64)
		}
		return t
	case []byte:
		if utf8.Valid(t) {
			return string(t)
		}
		return base64.StdEncoding.EncodeToString(t)
	case time.Time:
		return t.Format(time.RFC3339Nano)
	}
	return v // nil, int64, bool, string
}

// idText is a row id as text.
func idText(v any) string {
	switch t := v.(type) {
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case string:
		return t
	}
	return ""
}

// SQLUpdate marks a row a database source read: UPDATE Table SET each Set
// column to its value WHERE Key = the row id; identifiers are checked with
// ValidSQLIdentifier by the caller and quoted here, values are parameters.
type SQLUpdate struct {
	Dialect string
	Table   string
	Key     string
	Set     map[string]string
	Timeout time.Duration
}

// Statement is the update's SQL, columns in name order.
func (u SQLUpdate) Statement() (string, []string, error) {
	if !ValidSQLIdentifier(u.Table, true) || !ValidSQLIdentifier(u.Key, false) || len(u.Set) == 0 {
		return "", nil, errors.New("database: update needs a table, a key column, and at least one column to set")
	}
	names := make([]string, 0, len(u.Set))
	for n := range u.Set {
		if !ValidSQLIdentifier(n, false) {
			return "", nil, errors.New("database: invalid column name " + strconv.Quote(n))
		}
		names = append(names, n)
	}
	sort.Strings(names)
	sets := make([]string, len(names))
	for i, n := range names {
		sets[i] = quoteIdentifier(n) + " = " + placeholder(u.Dialect, i+1)
	}
	return "UPDATE " + quoteTable(u.Table) + " SET " + strings.Join(sets, ", ") + " WHERE " + quoteIdentifier(u.Key) + " = " + placeholder(u.Dialect, len(names)+1), names, nil
}

// Mark runs the update for the row with id, as one statement (its own
// transaction) within the timeout.
func (u SQLUpdate) Mark(ctx context.Context, db *sql.DB, id string) error {
	stmt, names, err := u.Statement()
	if err != nil {
		return err
	}
	args := make([]any, 0, len(names)+1)
	for _, n := range names {
		args = append(args, u.Set[n])
	}
	args = append(args, id)
	if u.Timeout <= 0 {
		u.Timeout = DBSinkTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, u.Timeout)
	defer cancel()
	if _, err := db.ExecContext(ctx, stmt, args...); err != nil {
		return dbError(ctx, u.Dialect, u.Timeout, err)
	}
	return nil
}
