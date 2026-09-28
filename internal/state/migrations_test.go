package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

// openTestDB opens a single-connection in-memory SQLite handle so the shared
// schema persists across calls on the same connection (mirrors OpenSQLite).
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestMigrateApplyErrorRollsBack guards the forward-only migration engine's
// failure handling: when a migration's Apply step fails, the transaction must
// be rolled back and no version recorded, so a corrected retry can re-run it.
func TestMigrateApplyErrorRollsBack(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	boom := errors.New("boom: schema step failed")
	migrations := []Migration{
		{
			Version: 1,
			Name:    "initial-schema",
			Apply: func(_ context.Context, _ *sql.Tx) error {
				return boom
			},
		},
	}

	err := Migrate(ctx, db, migrations)
	if err == nil {
		t.Fatal("Migrate() error = nil, want apply failure")
	}
	if !errors.Is(err, boom) {
		t.Errorf("Migrate() error = %v, want to wrap %v", err, boom)
	}
	if !strings.Contains(err.Error(), "migration 1 (initial-schema)") {
		t.Errorf("Migrate() error = %v, want migration context", err)
	}

	// The failed migration must not be recorded; a corrected retry would
	// still see version 0 and attempt it again.
	var rows int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&rows); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if rows != 0 {
		t.Errorf("schema_migrations has %d rows after failed apply, want 0", rows)
	}
}

// TestMigrateInsertErrorRollsBack guards the branch where Apply succeeds but
// recording the applied version fails (e.g. the version table was dropped by
// the migration itself); the transaction must roll back and surface the error.
func TestMigrateInsertErrorRollsBack(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	migrations := []Migration{
		{
			Version: 1,
			Name:    "drops-version-table",
			Apply: func(ctx context.Context, tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, `DROP TABLE schema_migrations`)
				return err
			},
		},
	}

	if err := Migrate(ctx, db, migrations); err == nil {
		t.Fatal("Migrate() error = nil, want version-record failure")
	}
}

// TestMigrateClosedDB guards the initial bootstrap failure branch: when the
// schema_migrations bootstrap fails (closed handle), Migrate returns it.
func TestMigrateClosedDB(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db, Migrations()); err == nil {
		t.Fatal("Migrate() on closed db error = nil, want non-nil")
	}
}

// TestMigrateForwardOnlySkipsApplied verifies that an already-applied version
// is never re-run even when its Apply would otherwise fail.
func TestMigrateForwardOnlySkipsApplied(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := Migrate(ctx, db, Migrations()); err != nil {
		t.Fatalf("seed migrate: %v", err)
	}

	// A chain where the already-applied version would explode if re-run.
	reRun := Migrations()
	reRun[0].Apply = func(_ context.Context, _ *sql.Tx) error {
		return errors.New("must not re-run applied migration")
	}
	if err := Migrate(ctx, db, reRun); err != nil {
		t.Errorf("Migrate() re-run error = %v, want no-op success", err)
	}
}

// TestMigrateOrderEnforced: a chain not numbered 1, 2, 3, … in order is
// refused before the database is touched.
func TestMigrateOrderEnforced(t *testing.T) {
	noop := func(context.Context, *sql.Tx) error { return nil }
	for name, chain := range map[string][]Migration{
		"starts at 2":  {{Version: 2, Name: "b", Apply: noop}},
		"gap":          {{Version: 1, Name: "a", Apply: noop}, {Version: 3, Name: "c", Apply: noop}},
		"duplicate":    {{Version: 1, Name: "a", Apply: noop}, {Version: 1, Name: "a2", Apply: noop}},
		"out of order": {{Version: 2, Name: "b", Apply: noop}, {Version: 1, Name: "a", Apply: noop}},
	} {
		t.Run(name, func(t *testing.T) {
			db := openTestDB(t)
			err := Migrate(context.Background(), db, chain)
			if err == nil || !strings.Contains(err.Error(), "must be numbered 1, 2, 3") {
				t.Fatalf("Migrate() = %v, want the order refused", err)
			}
			var n int
			if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'schema_migrations'`).Scan(&n); err != nil || n != 0 {
				t.Errorf("schema_migrations created (%d, %v): the database was touched", n, err)
			}
		})
	}
}

// TestMigrateNewerSchemaRefused: a database migrated by a newer release is
// refused, naming both versions and the release that wrote it, and is left
// unchanged; each applied migration records the weavster version.
func TestMigrateNewerSchemaRefused(t *testing.T) {
	ctx := context.Background()
	all := Migrations()
	for _, tt := range []struct{ name, writer, want string }{
		{"recorded writer", "9.9.9", "(written by weavster 9.9.9), newer than this release supports"},
		{"unknown writer", "", "(written by an unknown weavster release), newer than this release supports (8): run a newer weavster release,"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db := openTestDB(t)
			old := AppVersion
			AppVersion = tt.writer
			t.Cleanup(func() { AppVersion = old })
			if err := Migrate(ctx, db, all); err != nil {
				t.Fatal(err)
			}
			var app string
			if err := db.QueryRow(`SELECT app_version FROM schema_migrations WHERE version = ?`, len(all)).Scan(&app); err != nil || app != tt.writer {
				t.Errorf("app_version = %q (%v), want %q", app, err, tt.writer)
			}
			AppVersion = "1.0.0"
			err := Migrate(ctx, db, all[:len(all)-1])
			want := fmt.Sprintf("the database schema is at version %d %s", len(all), tt.want)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("Migrate() = %v, want %q", err, want)
			}
			var n int
			if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil || n != len(all) {
				t.Errorf("schema_migrations has %d rows (%v) after the refusal, want %d", n, err, len(all))
			}
		})
	}
}

// migrationBackends are the raw databases migration tests run on: SQLite,
// and PostgreSQL with WEAVSTER_TEST_POSTGRES_DSN (a fresh schema each).
func migrationBackends() map[string]func(t *testing.T) (*sql.DB, bool) {
	backends := map[string]func(t *testing.T) (*sql.DB, bool){
		"sqlite": func(t *testing.T) (*sql.DB, bool) { return openTestDB(t), false },
	}
	if os.Getenv("WEAVSTER_TEST_POSTGRES_DSN") != "" {
		backends["postgres"] = func(t *testing.T) (*sql.DB, bool) {
			db, err := sql.Open("pgx", testPostgresDSN(t))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			return db, true
		}
	}
	return backends
}

// oldSchemaMigrations makes db look like a database from before
// app_version was recorded, at schema version v (its rows only).
func oldSchemaMigrations(t *testing.T, db *sql.DB, v int) {
	t.Helper()
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= v; i++ {
		if _, err := db.Exec(fmt.Sprintf(`INSERT INTO schema_migrations (version, name) VALUES (%d, 'm%d')`, i, i)); err != nil {
			t.Fatal(err)
		}
	}
}

// TestMigrateAddsAppVersionColumn: a database from before app_version was
// recorded gets the column, and its earlier rows keep "".
func TestMigrateAddsAppVersionColumn(t *testing.T) {
	ctx := context.Background()
	all := Migrations()
	for name, open := range migrationBackends() {
		t.Run(name, func(t *testing.T) {
			db, _ := open(t)
			oldSchemaMigrations(t, db, 1)
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			if err := all[0].Apply(ctx, tx); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			if err := Migrate(ctx, db, all); err != nil {
				t.Fatal(err)
			}
			var first, second string
			if err := db.QueryRow(`SELECT (SELECT app_version FROM schema_migrations WHERE version = 1), (SELECT app_version FROM schema_migrations WHERE version = 2)`).Scan(&first, &second); err != nil || first != "" || second != AppVersion {
				t.Errorf("app_version of 1 and 2 = %q, %q (%v); want \"\", %q", first, second, err, AppVersion)
			}
		})
	}
}

// TestMigrateNewerSchemaUntouched: a newer database is refused before any
// change, even one from before app_version was recorded.
func TestMigrateNewerSchemaUntouched(t *testing.T) {
	for name, open := range migrationBackends() {
		t.Run(name, func(t *testing.T) {
			db, _ := open(t)
			oldSchemaMigrations(t, db, len(Migrations())+1)
			var newer *NewerSchemaError
			if err := Migrate(context.Background(), db, Migrations()); !errors.As(err, &newer) || newer.Version != len(Migrations())+1 {
				t.Fatalf("Migrate() = %v, want a NewerSchemaError", err)
			}
			if _, err := db.Exec(`SELECT app_version FROM schema_migrations`); err == nil {
				t.Error("app_version was added to the refused database")
			}
		})
	}
}

// TestOpenClosesHandleOnMigrationFailure: when migrating fails (here, the
// database refuses the first query) the store closes its handle.
func TestOpenClosesHandleOnMigrationFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(`pragma_table_info`).WillReturnError(errors.New("permission denied for schema public"))
	mock.ExpectClose()
	if _, err := openSQLStore(context.Background(), db, false); err == nil {
		t.Fatal("openSQLStore() = nil error, want the migration failure")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// TestUpgradeFromEveryVersion: a database left at each earlier schema
// version, holding data written at that version, upgrades to the latest
// with the data kept, on SQLite and (with WEAVSTER_TEST_POSTGRES_DSN)
// PostgreSQL.
func TestUpgradeFromEveryVersion(t *testing.T) {
	ctx := context.Background()
	all := Migrations()
	backends := migrationBackends()
	// Data each version can hold, written with that version's columns.
	fixtures := []struct {
		since int
		stmt  string
	}{
		{1, `INSERT INTO messages (id, flow_id, status, content_type, received_at, updated_at, raw) VALUES ('m1', 'adt', 'queued', 'raw', 1, 1, 'MSH|x')`},
		{1, `INSERT INTO message_metadata (message_id, key, value) VALUES ('m1', 'source', 'mllp')`},
		{1, `INSERT INTO message_attempts (message_id, destination, attempts, last_error) VALUES ('m1', 'ehr', 2, 'Service Unavailable')`},
		{2, `INSERT INTO flows (id, document) VALUES ('adt', '{"id":"adt","name":"ADT"}')`},
	}
	for name, open := range backends {
		for k := 0; k < len(all); k++ {
			t.Run(fmt.Sprintf("%s/from-%d", name, k), func(t *testing.T) {
				db, postgres := open(t)
				if err := Migrate(ctx, db, all[:k]); err != nil {
					t.Fatal(err)
				}
				for _, f := range fixtures {
					if f.since <= k {
						if _, err := db.ExecContext(ctx, f.stmt); err != nil {
							t.Fatal(err)
						}
					}
				}
				s, err := openSQLStore(ctx, db, postgres)
				if err != nil {
					t.Fatalf("upgrade from %d: %v", k, err)
				}
				var v int
				if err := db.QueryRowContext(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&v); err != nil || v != len(all) {
					t.Fatalf("schema version %d (%v), want %d", v, err, len(all))
				}
				if k < 1 {
					return
				}
				m, err := s.Get(ctx, "m1")
				if err != nil || string(m.Raw) != "MSH|x" || m.Metadata["source"] != "mllp" ||
					m.Attempts["ehr"].Attempts != 2 || m.Attempts["ehr"].LastError != "Service Unavailable" {
					t.Errorf("message after the upgrade: %+v %v", m, err)
				}
				if k >= 2 {
					if f, err := s.GetFlow(ctx, "adt"); err != nil || !strings.Contains(string(f.Document), `"ADT"`) {
						t.Errorf("flow after the upgrade: %+v %v", f, err)
					}
				}
			})
		}
	}
}
