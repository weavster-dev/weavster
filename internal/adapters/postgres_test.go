package adapters

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// testPostgresDB opens a fresh schema of the PostgreSQL database
// WEAVSTER_TEST_POSTGRES_DSN names (the CI PostgreSQL job sets it), or
// skips: no test needs PostgreSQL to run.
func testPostgresDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("WEAVSTER_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("WEAVSTER_TEST_POSTGRES_DSN is not set")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	schema := "t_" + hex.EncodeToString(b)
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
		_ = admin.Close()
	})
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestPostgresDatabaseAdapters: against PostgreSQL 16, a database
// destination inserts text values that PostgreSQL converts to each column's
// type (integer, numeric with every digit, boolean, timestamptz, jsonb),
// with keyColumn a retry inserts nothing, PostgreSQL errors are described by
// SQLSTATE; a database source's query runs read-only and its update marks
// rows with $n parameters.
func TestPostgresDatabaseAdapters(t *testing.T) {
	db := testPostgresDB(t)
	ctx := context.Background()
	var schema string // a schema.table name, in the test's own schema
	if err := db.QueryRow(`SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE results (n integer, amount numeric, ok boolean, at timestamptz, doc jsonb, label text, k text UNIQUE)`); err != nil {
		t.Fatal(err)
	}
	sink, err := NewSQLSink(db, SQLSinkOptions{Dialect: DialectPostgres, Table: schema + ".results", KeyColumn: "k", Columns: []SQLColumn{
		{"n", "n"}, {"amount", "amount"}, {"ok", "ok"}, {"at", "at"}, {"doc", "doc"}, {"label", "label"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	msg := Message{Body: []byte(`{"n":42,"amount":12345678901234567890.123,"ok":true,"at":"2026-09-28T10:00:00Z","doc":{"a":[1]},"label":"O'Brien"}`),
		Metadata: map[string]string{IdempotencyKeyMetadata: "key-1"}}
	for i := 0; i < 2; i++ {
		if err := sink.Write(ctx, msg); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	var n, count int
	var amount, label, doc string
	var ok bool
	if err := db.QueryRow(`SELECT n, amount::text, ok, doc::text, label, (SELECT count(*) FROM results) FROM results`).Scan(&n, &amount, &ok, &doc, &label, &count); err != nil {
		t.Fatal(err)
	}
	if n != 42 || amount != "12345678901234567890.123" || !ok || doc != `{"a": [1]}` || label != "O'Brien" || count != 1 {
		t.Errorf("row: %d %s %v %s %q, %d rows", n, amount, ok, doc, label, count)
	}
	bad := Message{Body: []byte(`{"n":"not a number"}`), Metadata: map[string]string{IdempotencyKeyMetadata: "key-2"}}
	if err := sink.Write(ctx, bad); err == nil || !strings.Contains(err.Error(), "SQLSTATE 22P02") || ErrorCode(err) != "sqlstate:22P02" || strings.Contains(err.Error(), "not a number") {
		t.Errorf("a value the column cannot take: %v", err)
	}
	missing, _ := NewSQLSink(db, SQLSinkOptions{Dialect: DialectPostgres, Table: "nope", Columns: []SQLColumn{{"a", "a"}}})
	if err := missing.Write(ctx, Message{Body: []byte(`{}`)}); ErrorCode(err) != "sqlstate:42P01" {
		t.Errorf("missing table: %v", err)
	}

	if _, err := db.Exec(`CREATE TABLE orders (id integer PRIMARY KEY, exported boolean NOT NULL DEFAULT false); INSERT INTO orders (id) VALUES (1), (2)`); err != nil {
		t.Fatal(err)
	}
	rows, more, err := QuerySQL(ctx, db, SQLQueryOptions{Dialect: DialectPostgres, Query: "SELECT id FROM orders WHERE NOT exported ORDER BY id", IDColumn: "id", MaxRows: 1})
	if err != nil || len(rows) != 1 || rows[0].ID != "1" || !more {
		t.Fatalf("query: %+v %v %v", rows, more, err)
	}
	if _, err := db.Exec(`CREATE SEQUENCE counter`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := QuerySQL(ctx, db, SQLQueryOptions{Dialect: DialectPostgres, Query: "SELECT id, nextval('counter') FROM orders", IDColumn: "id", MaxRows: 1}); ErrorCode(err) != "sqlstate:25006" {
		t.Errorf("a query that writes (nextval) in the read-only transaction: %v, want SQLSTATE 25006", err)
	}
	mark := SQLUpdate{Dialect: DialectPostgres, Table: "orders", Key: "id", Set: map[string]string{"exported": "true"}}
	if err := mark.Mark(ctx, db, "1"); err != nil {
		t.Fatal(err)
	}
	var exported bool
	if err := db.QueryRow(`SELECT exported FROM orders WHERE id = 1`).Scan(&exported); err != nil || !exported {
		t.Errorf("marked: %v %v", exported, err)
	}
}
