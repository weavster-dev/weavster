package state

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
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
	reRun := []Migration{
		{
			Version: 1,
			Name:    "initial-schema",
			Apply: func(_ context.Context, _ *sql.Tx) error {
				return errors.New("must not re-run applied migration")
			},
		},
	}
	if err := Migrate(ctx, db, reRun); err != nil {
		t.Errorf("Migrate() re-run error = %v, want no-op success", err)
	}
}
