package state

import (
	"context"
	"database/sql"
	"testing"
)

// TestRebind: "?" placeholders become $1, $2, … for PostgreSQL only.
func TestRebind(t *testing.T) {
	for _, tt := range []struct {
		postgres bool
		in, want string
	}{
		{true, `SELECT a FROM t WHERE b = ? AND c IN (?, ?)`, `SELECT a FROM t WHERE b = $1 AND c IN ($2, $3)`},
		{true, `SELECT 1`, `SELECT 1`},
		{false, `SELECT a FROM t WHERE b = ?`, `SELECT a FROM t WHERE b = ?`},
		{true, `SELECT id FROM t WHERE id /*C*/ > ? ORDER BY id /*C*/`, `SELECT id FROM t WHERE id COLLATE "C" > $1 ORDER BY id COLLATE "C"`},
		{false, `SELECT id FROM t ORDER BY id /*C*/`, `SELECT id FROM t ORDER BY id /*C*/`},
	} {
		if got := rebind(tt.postgres, tt.in); got != tt.want {
			t.Errorf("rebind(%v, %q) = %q, want %q", tt.postgres, tt.in, got, tt.want)
		}
	}
}

// TestDialectDetection: a SQLite database is not PostgreSQL; a pgx one is
// (no server needed to open it).
func TestDialectDetection(t *testing.T) {
	lite, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lite.Close() }()
	pg, err := sql.Open("pgx", "postgres://nobody@127.0.0.1:1/none")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pg.Close() }()
	if isPostgres(lite) || !isPostgres(pg) {
		t.Errorf("isPostgres: sqlite %v, pgx %v", isPostgres(lite), isPostgres(pg))
	}
	d := &dialectDB{db: lite}
	tx, err := d.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(context.Background(), `CREATE TABLE x (a TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), `INSERT INTO x VALUES (?)`, "v"); err != nil {
		t.Fatal(err)
	}
	var v string
	if err := tx.QueryRowContext(context.Background(), `SELECT a FROM x WHERE a = ?`, "v").Scan(&v); err != nil || v != "v" {
		t.Errorf("through a transaction: %q %v", v, err)
	}
	rows, err := tx.QueryContext(context.Background(), `SELECT a FROM x WHERE a = ?`, "v")
	if err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	st, err := tx.PrepareContext(context.Background(), `SELECT a FROM x WHERE a = ?`)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
}

// TestTextValue: text is stored without NUL bytes and as valid UTF-8, on
// every backend alike.
func TestTextValue(t *testing.T) {
	for in, want := range map[string]string{"a\x00b": "ab", "caf\xe9": "caf\uFFFD", "ok": "ok"} {
		if got := textValue(in); got != want {
			t.Errorf("textValue(%q) = %q, want %q", in, got, want)
		}
	}
	ctx := context.Background()
	for name, s := range testBackends(t) {
		m := Message{ID: "nul", FlowID: "f", Status: StatusQueued, Metadata: map[string]string{"source.file": "a\x00b\xff"},
			Attempts: map[string]DestinationAttempt{"out": {Attempts: 1, LastError: "ACK \xe9\x00"}}}
		if err := s.Put(ctx, m); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, err := s.Get(ctx, "nul")
		if err != nil || got.Metadata["source.file"] != "ab\uFFFD" || got.Attempts["out"].LastError != "ACK \uFFFD" {
			t.Errorf("%s: %+v %v", name, got, err)
		}
	}
}
