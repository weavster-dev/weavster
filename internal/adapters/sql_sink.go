package adapters

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/weavster-dev/weavster/internal/dsl"
)

// SQL dialects a database destination writes.
const (
	DialectPostgres = "postgres"
	DialectSQLite   = "sqlite"
)

// sqlIdentifier is a table or column name a database destination accepts:
// letters, digits, and underscores, not starting with a digit (#107 D-75).
var sqlIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

// ValidSQLIdentifier reports whether name can be a column name, and a
// table name when written as table or schema.table.
func ValidSQLIdentifier(name string, qualified bool) bool {
	parts := []string{name}
	if qualified {
		parts = strings.SplitN(name, ".", 2)
	}
	for _, p := range parts {
		if !sqlIdentifier.MatchString(p) {
			return false
		}
	}
	return true
}

// SQLColumn is a column a database destination fills from the message:
// the value at Path (dot-separated keys; numbers index arrays).
type SQLColumn struct {
	Name, Path string
}

// SQLSinkOptions describe a database destination's insert: Table and the
// Columns (identifiers checked by the caller with ValidSQLIdentifier), and
// KeyColumn, which receives the delivery's idempotency key so a retry
// inserts nothing ("" for none).
type SQLSinkOptions struct {
	Dialect   string
	Table     string
	Columns   []SQLColumn
	KeyColumn string
	// Timeout bounds one insert (DBSinkTimeout when not positive).
	Timeout time.Duration
}

// DBSinkTimeout bounds one insert unless SQLSinkOptions.Timeout says
// otherwise.
const DBSinkTimeout = 30 * time.Second

// SQLSink inserts each message, a JSON object, as one row (#107 D-75).
type SQLSink struct {
	db      *sql.DB
	dialect string
	columns []SQLColumn
	withKey bool
	insert  string
	timeout time.Duration
}

// NewSQLSink prepares the insert for db (which the caller owns).
func NewSQLSink(db *sql.DB, o SQLSinkOptions) (*SQLSink, error) {
	if o.Dialect != DialectPostgres && o.Dialect != DialectSQLite {
		return nil, fmt.Errorf("database: unknown dialect %q", o.Dialect)
	}
	if !ValidSQLIdentifier(o.Table, true) || len(o.Columns) == 0 {
		return nil, errors.New("database: a table and at least one column are required")
	}
	names := make([]string, 0, len(o.Columns)+1)
	for _, c := range o.Columns {
		if !ValidSQLIdentifier(c.Name, false) {
			return nil, fmt.Errorf("database: invalid column name %q", c.Name)
		}
		names = append(names, c.Name)
	}
	if o.KeyColumn != "" {
		if !ValidSQLIdentifier(o.KeyColumn, false) {
			return nil, fmt.Errorf("database: invalid key column %q", o.KeyColumn)
		}
		names = append(names, o.KeyColumn)
	}
	quoted := make([]string, len(names))
	params := make([]string, len(names))
	for i, n := range names {
		quoted[i] = quoteIdentifier(n)
		params[i] = "?"
		if o.Dialect == DialectPostgres {
			params[i] = "$" + strconv.Itoa(i+1)
		}
	}
	table := make([]string, 0, 2)
	for _, p := range strings.SplitN(o.Table, ".", 2) {
		table = append(table, quoteIdentifier(p))
	}
	insert := "INSERT INTO " + strings.Join(table, ".") + " (" + strings.Join(quoted, ", ") + ") VALUES (" + strings.Join(params, ", ") + ")"
	if o.KeyColumn != "" {
		insert += " ON CONFLICT (" + quoteIdentifier(o.KeyColumn) + ") DO NOTHING"
	}
	if o.Timeout <= 0 {
		o.Timeout = DBSinkTimeout
	}
	return &SQLSink{db: db, dialect: o.Dialect, columns: o.Columns, withKey: o.KeyColumn != "", insert: insert, timeout: o.Timeout}, nil
}

// quoteIdentifier double-quotes a checked identifier (standard SQL, and
// both dialects); quoted names are case-sensitive in PostgreSQL.
func quoteIdentifier(name string) string { return `"` + name + `"` }

func (s *SQLSink) Name() string { return "database" }

// Statement is the insert the sink runs, for inspection.
func (s *SQLSink) Statement() string { return s.insert }

// Write inserts m with one statement, its own transaction, within the
// sink's timeout. Errors name the database's error class, never values
// from the message.
func (s *SQLSink) Write(ctx context.Context, m Message) error {
	args, err := s.values(m)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	if _, err := s.db.ExecContext(ctx, s.insert, args...); err != nil {
		return s.dbError(ctx, err)
	}
	return nil
}

// values are the insert's arguments: each column's value, then the key.
func (s *SQLSink) values(m Message) ([]any, error) {
	dec := json.NewDecoder(bytes.NewReader(m.Body))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil || doc == nil {
		return nil, errors.New("database: the message is not a JSON object")
	}
	args := make([]any, 0, len(s.columns)+1)
	for _, c := range s.columns {
		v, _ := dsl.Lookup(doc, c.Path) // missing: NULL
		args = append(args, sqlValue(v))
	}
	if s.withKey {
		key := m.Metadata[IdempotencyKeyMetadata]
		if key == "" {
			return nil, errors.New("database: the delivery has no idempotency key")
		}
		args = append(args, key)
	}
	return args, nil
}

// sqlValue is a JSON value as a query parameter: its text, which the
// database converts to the column's type (PostgreSQL parses "42" into an
// integer, "true" into a boolean, an ISO date into a timestamp; SQLite
// applies the column's affinity). Numbers keep every digit as written;
// objects and arrays are their JSON text; null and missing values are
// NULL. Text is the one form pgx encodes into every column type.
func sqlValue(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	default:
		b, _ := json.Marshal(t) // decoded JSON always marshals
		return string(b)
	}
}

// dbError describes a database error without its details, which can quote
// values: PostgreSQL errors by SQLSTATE class, client-side failures by
// kind, and SQLite's by their message (it names tables and columns, not
// values).
func (s *SQLSink) dbError(ctx context.Context, err error) error {
	var pg *pgconn.PgError
	var connect *pgconn.ConnectError
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("database: no result within %s", s.timeout)
	case ctx.Err() != nil:
		return errors.New("database: the delivery was cancelled")
	case errors.As(err, &connect):
		return errors.New("database: could not connect (check the host, credentials, and TLS settings in the connection string)")
	case errors.Is(err, sql.ErrConnDone):
		return errors.New("database: the connection was closed")
	case errors.As(err, &pg):
	case s.dialect == DialectPostgres:
		return errors.New("database: the statement failed before reaching the database")
	default:
		return fmt.Errorf("database: %w", err)
	}
	what := map[string]string{
		"42P01": "the table does not exist",
		"42703": "a column does not exist",
		"23505": "a unique constraint was violated",
		"23502": "a column that must not be NULL got NULL",
		"22P02": "a value does not fit its column's type",
		"42P10": "keyColumn has no unique constraint",
		"28P01": "the login was refused",
		"42501": "permission denied",
	}[pg.Code]
	if what == "" {
		what = "the statement failed"
	}
	return fmt.Errorf("database: %s (SQLSTATE %s)", what, pg.Code)
}

func (s *SQLSink) Close() error { return nil }
