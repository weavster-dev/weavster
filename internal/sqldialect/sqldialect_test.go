package sqldialect

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite" // the SQLite driver, to tell it apart
)

func TestRebind(t *testing.T) {
	for _, tt := range []struct {
		postgres bool
		in, want string
	}{
		{true, `SELECT a FROM t WHERE b = ? AND c = ?`, `SELECT a FROM t WHERE b = $1 AND c = $2`},
		{true, `SELECT id FROM t ORDER BY id /*C*/`, `SELECT id FROM t ORDER BY id COLLATE "C"`},
		{true, `SELECT 1`, `SELECT 1`},
		{false, `SELECT a FROM t WHERE b = ? ORDER BY id /*C*/`, `SELECT a FROM t WHERE b = ? ORDER BY id /*C*/`},
	} {
		if got := Rebind(tt.postgres, tt.in); got != tt.want {
			t.Errorf("Rebind(%v, %q) = %q, want %q", tt.postgres, tt.in, got, tt.want)
		}
	}
}

func TestIsPostgres(t *testing.T) {
	lite, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lite.Close() }()
	pg, err := sql.Open("pgx", "postgres://nobody@127.0.0.1:1/none") // not connected
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pg.Close() }()
	if IsPostgres(lite) || !IsPostgres(pg) {
		t.Errorf("IsPostgres: sqlite %v, pgx %v", IsPostgres(lite), IsPostgres(pg))
	}
}
