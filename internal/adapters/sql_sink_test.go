package adapters

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "modernc.org/sqlite"
)

// TestSQLSink: each message becomes one row, values bound as parameters;
// with a key column a retry inserts nothing.
func TestSQLSink(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE results (mrn TEXT, value REAL, n INTEGER, ok INTEGER, extra TEXT, k TEXT UNIQUE)`); err != nil {
		t.Fatal(err)
	}
	s, err := NewSQLSink(db, SQLSinkOptions{Dialect: DialectSQLite, Table: "results", KeyColumn: "k", Columns: []SQLColumn{
		{"mrn", "patient.ids.0"}, {"value", "obs.value"}, {"n", "count"}, {"ok", "flag"}, {"extra", "obj"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	msg := Message{Body: []byte(`{"patient":{"ids":["123'; DROP TABLE results;--"]},"obs":{"value":5.4},"count":12345678901,"flag":true,"obj":{"a":[1]}}`),
		Metadata: map[string]string{IdempotencyKeyMetadata: "key-1"}}
	for i := 0; i < 2; i++ { // the second write is a retry
		if err := s.Write(ctx, msg); err != nil {
			t.Fatal(err)
		}
	}
	var mrn, extra string
	var value float64
	var n int64
	var ok bool
	var count int
	if err := db.QueryRow(`SELECT mrn, value, n, ok, extra, (SELECT count(*) FROM results) FROM results`).Scan(&mrn, &value, &n, &ok, &extra, &count); err != nil {
		t.Fatal(err)
	}
	if mrn != "123'; DROP TABLE results;--" || value != 5.4 || n != 12345678901 || !ok || extra != `{"a":[1]}` || count != 1 {
		t.Errorf("row = %q %v %v %v %q, %d rows", mrn, value, n, ok, extra, count)
	}
	if err := s.Write(ctx, Message{Body: []byte(`{}`), Metadata: map[string]string{IdempotencyKeyMetadata: "key-2"}}); err != nil {
		t.Errorf("missing paths are NULL: %v", err)
	}
	for body, want := range map[string]string{
		`[1]`: "not a JSON object", `nope`: "not a JSON object", `null`: "not a JSON object", `{"a":1} trailing`: "not a single JSON object",
	} {
		if err := s.Write(ctx, Message{Body: []byte(body), Metadata: map[string]string{IdempotencyKeyMetadata: "k"}}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", body, err, want)
		}
	}
	if err := s.Write(ctx, Message{Body: []byte(`{}`)}); err == nil || !strings.Contains(err.Error(), "no idempotency key") {
		t.Errorf("without a key: %v", err)
	}
	missing, _ := NewSQLSink(db, SQLSinkOptions{Dialect: DialectSQLite, Table: "nope", Columns: []SQLColumn{{"a", "a"}}})
	if err := missing.Write(ctx, Message{Body: []byte(`{}`)}); err == nil || !strings.Contains(err.Error(), "no such table") {
		t.Errorf("missing table: %v", err)
	}
}

// TestSQLSinkStatement: dialect-correct placeholders and quoted
// identifiers; invalid identifiers are refused.
func TestSQLSinkStatement(t *testing.T) {
	cols := []SQLColumn{{"mrn", "a"}, {"Value", "b"}}
	for _, tt := range []struct {
		o    SQLSinkOptions
		want string
	}{
		{SQLSinkOptions{Dialect: DialectPostgres, Table: "lab.results", Columns: cols, KeyColumn: "k"},
			`INSERT INTO "lab"."results" ("mrn", "Value", "k") VALUES ($1, $2, $3) ON CONFLICT ("k") DO NOTHING`},
		{SQLSinkOptions{Dialect: DialectSQLite, Table: "results", Columns: cols},
			`INSERT INTO "results" ("mrn", "Value") VALUES (?, ?)`},
	} {
		s, err := NewSQLSink(nil, tt.o)
		if err != nil || s.Statement() != tt.want {
			t.Errorf("%s: %q %v\nwant %q", tt.o.Dialect, s.Statement(), err, tt.want)
		}
	}
	for _, o := range []SQLSinkOptions{
		{Dialect: "mysql", Table: "t", Columns: cols},
		{Dialect: DialectSQLite, Table: `t"; DROP`, Columns: cols},
		{Dialect: DialectSQLite, Table: "a.b.c", Columns: cols},
		{Dialect: DialectSQLite, Table: "t"},
		{Dialect: DialectSQLite, Table: "t", Columns: []SQLColumn{{"1x", "a"}}},
		{Dialect: DialectSQLite, Table: "t", Columns: cols, KeyColumn: "k-1"},
	} {
		if _, err := NewSQLSink(nil, o); err == nil {
			t.Errorf("%+v: accepted", o)
		}
	}
}

// TestSQLSinkErrors: PostgreSQL errors are described by SQLSTATE class,
// never with the server's text (it can quote values).
func TestSQLSinkErrors(t *testing.T) {
	pg, _ := NewSQLSink(nil, SQLSinkOptions{Dialect: DialectPostgres, Table: "t", Columns: []SQLColumn{{"a", "a"}}})
	for err, want := range map[error]string{
		&pgconn.PgError{Code: "23505", Message: "dup", Detail: "Key (mrn)=(123) already exists"}: "a unique constraint was violated (SQLSTATE 23505)",
		&pgconn.PgError{Code: "22P02", Message: `invalid input syntax for type integer: "DOE"`}:  "a value does not fit its column's type (SQLSTATE 22P02)",
		&pgconn.PgError{Code: "XX000"}:        "the statement failed (SQLSTATE XX000)",
		errors.New("encode: password=secret"): "failed before reaching the database",
		&pgconn.ConnectError{}:                "could not connect",
		sql.ErrConnDone:                       "the connection was closed",
	} {
		got := pg.dbError(context.Background(), err).Error()
		if !strings.Contains(got, want) || strings.Contains(got, "123") || strings.Contains(got, "DOE") || strings.Contains(got, "secret") {
			t.Errorf("%v: %q, want %q", err, got, want)
		}
	}
	expired, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	if got := pg.dbError(expired, errors.New("x")).Error(); !strings.Contains(got, "no result within 30s") {
		t.Errorf("timeout: %q", got)
	}
	gone, stop := context.WithCancel(context.Background())
	stop()
	if got := pg.dbError(gone, errors.New("x")).Error(); !strings.Contains(got, "cancelled") {
		t.Errorf("cancelled: %q", got)
	}
	if !ValidSQLIdentifier("a.b", true) || ValidSQLIdentifier("a.b", false) || ValidSQLIdentifier("", false) {
		t.Error("ValidSQLIdentifier")
	}
}

// TestSQLValue: every value is sent as text (numbers with every digit),
// objects and arrays as JSON, null as NULL.
func TestSQLValue(t *testing.T) {
	for _, tt := range []struct {
		in   any
		want any
	}{
		{nil, nil}, {"a", "a"}, {json.Number("18446744073709551615"), "18446744073709551615"}, {json.Number("5.40"), "5.40"},
		{true, "true"}, {map[string]any{"a": []any{json.Number("1")}}, `{"a":[1]}`},
	} {
		if got := sqlValue(tt.in); got != tt.want {
			t.Errorf("%v: %v, want %v", tt.in, got, tt.want)
		}
	}
	s, _ := NewSQLSink(nil, SQLSinkOptions{Dialect: DialectSQLite, Table: "t", Columns: []SQLColumn{{"a", "a"}}, Timeout: time.Second})
	if s.timeout != time.Second {
		t.Errorf("timeout %s", s.timeout)
	}
}
