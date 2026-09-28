package adapters

import (
	"context"
	"database/sql"
	"math"
	"strings"
	"testing"
	"time"
)

// TestQuerySQL: rows come back as JSON values with their id, at most
// MaxRows; the query runs read-only, so a statement that writes fails; the
// connection is writable again afterwards.
func TestQuerySQL(t *testing.T) {
	db, err := sql.Open("sqlite", t.TempDir()+"/src.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE t (id TEXT, n INTEGER, f REAL, s TEXT, b BLOB, z TEXT);
		INSERT INTO t VALUES ('a', 1, 1.5, 'x', x'00ff', NULL), ('b', 2, 2.5, 'y', 'text', NULL), ('c', 3, 3.5, 'z', NULL, NULL)`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	rows, err := QuerySQL(ctx, db, SQLQueryOptions{Dialect: DialectSQLite, Query: "SELECT id, n, f, s, b, z FROM t ORDER BY id;", IDColumn: "id", MaxRows: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "a" || rows[0].Values["n"] != int64(1) || rows[0].Values["f"] != 1.5 || rows[0].Values["b"] != "AP8=" ||
		rows[0].Values["z"] != nil || rows[1].Values["b"] != "text" {
		t.Errorf("rows = %+v", rows)
	}
	for _, tt := range []struct {
		o    SQLQueryOptions
		want string
	}{
		{SQLQueryOptions{Dialect: DialectSQLite, Query: "DELETE FROM t", IDColumn: "id", MaxRows: 1}, "one SELECT or WITH"},
		{SQLQueryOptions{Dialect: DialectSQLite, Query: "SELECT n FROM t", IDColumn: "id", MaxRows: 1}, `no idColumn "id"`},
		{SQLQueryOptions{Dialect: DialectSQLite, Query: "SELECT z AS id FROM t", IDColumn: "id", MaxRows: 1}, "idColumn is NULL"},
		{SQLQueryOptions{Dialect: DialectSQLite, Query: "SELECT id FROM nope", IDColumn: "id", MaxRows: 1}, "no such table"},
	} {
		if _, err := QuerySQL(ctx, db, tt.o); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v, want %q", tt.o.Query, err, tt.want)
		}
	}
	if ro, err := QuerySQL(ctx, db, SQLQueryOptions{Dialect: DialectSQLite, Query: "SELECT query_only AS id FROM pragma_query_only", IDColumn: "id", MaxRows: 1}); err != nil || len(ro) != 1 || ro[0].ID != "1" {
		t.Errorf("the query did not run read-only: %+v %v", ro, err)
	}
	if _, err := db.Exec(`INSERT INTO t (id) VALUES ('d')`); err != nil {
		t.Errorf("the connection stayed read-only: %v", err)
	}

	u := SQLUpdate{Dialect: DialectSQLite, Table: "t", Key: "id", Set: map[string]string{"s": "done", "n": "9"}, Timeout: time.Second}
	if err := u.Mark(ctx, db, "a"); err != nil {
		t.Fatal(err)
	}
	var s string
	var n int
	if err := db.QueryRow(`SELECT s, n FROM t WHERE id = 'a'`).Scan(&s, &n); err != nil || s != "done" || n != 9 {
		t.Errorf("marked row: %q %d %v", s, n, err)
	}
	pg := SQLUpdate{Dialect: DialectPostgres, Table: "his.orders", Key: "id", Set: map[string]string{"b": "1", "a": "x"}}
	if stmt, _, err := pg.Statement(); err != nil || stmt != `UPDATE "his"."orders" SET "a" = $1, "b" = $2 WHERE "id" = $3` {
		t.Errorf("postgres update: %q %v", stmt, err)
	}
	for _, bad := range []SQLUpdate{{Table: "t;", Key: "id", Set: map[string]string{"a": "1"}}, {Table: "t", Key: "i d", Set: map[string]string{"a": "1"}},
		{Table: "t", Key: "id"}, {Table: "t", Key: "id", Set: map[string]string{"a-b": "1"}}} {
		if err := bad.Mark(ctx, db, "a"); err == nil {
			t.Errorf("%+v: accepted", bad)
		}
	}
	if err := (SQLUpdate{Dialect: DialectSQLite, Table: "nope", Key: "id", Set: map[string]string{"a": "1"}}).Mark(ctx, db, "a"); err == nil || !strings.Contains(err.Error(), "no such table") {
		t.Errorf("missing table: %v", err)
	}
}

// TestJSONValue: column values as JSON values; ids as text.
func TestJSONValue(t *testing.T) {
	at := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	for _, tt := range []struct{ in, want any }{
		{math.Inf(1), "+Inf"}, {1.5, 1.5}, {[]byte("ok"), "ok"}, {[]byte{0xff}, "/w=="}, {at, "2026-09-28T10:00:00Z"}, {int64(3), int64(3)}, {nil, nil},
	} {
		if got := jsonValue(tt.in); got != tt.want {
			t.Errorf("%v: %v, want %v", tt.in, got, tt.want)
		}
	}
	for in, want := range map[any]string{int64(7): "7", 2.5: "2.5", true: "true", "x": "x", nil: ""} {
		if got := idText(in); got != want {
			t.Errorf("idText(%v) = %q", in, got)
		}
	}
	for q, ok := range map[string]bool{"select 1": true, " WITH a AS (SELECT 1) SELECT * FROM a;": true, "SELECT 1; SELECT 2": false, "UPDATE t SET a=1": false, "selectx": false} {
		if ValidSelect(q) != ok {
			t.Errorf("ValidSelect(%q) != %v", q, ok)
		}
	}
}
