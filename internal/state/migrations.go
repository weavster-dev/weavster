package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Migration is a single forward-only schema step (gap #7).
type Migration struct {
	Version int
	Name    string
	Apply   func(ctx context.Context, tx *sql.Tx) error
}

// Migrations returns the ordered, forward-only migration chain.
func Migrations() []Migration {
	return []Migration{
		{
			Version: 1,
			Name:    "initial-schema",
			Apply: func(ctx context.Context, tx *sql.Tx) error {
				stmts := []string{
					`CREATE TABLE IF NOT EXISTS messages (
						id TEXT PRIMARY KEY,
						flow_id TEXT NOT NULL,
						status TEXT NOT NULL,
						content_type TEXT NOT NULL DEFAULT 'raw',
						received_at BIGINT NOT NULL,
						updated_at BIGINT NOT NULL,
						raw BYTEA, processed BYTEA, transformed BYTEA,
						encoded BYTEA, response BYTEA, original BYTEA
					)`,
					`CREATE TABLE IF NOT EXISTS message_metadata (
						message_id TEXT NOT NULL,
						key TEXT NOT NULL,
						value TEXT NOT NULL,
						PRIMARY KEY (message_id, key)
					)`,
					`CREATE TABLE IF NOT EXISTS message_attempts (
						message_id TEXT NOT NULL,
						destination TEXT NOT NULL,
						attempts INTEGER NOT NULL,
						last_error TEXT NOT NULL DEFAULT '',
						PRIMARY KEY (message_id, destination)
					)`,
				}
				for _, s := range stmts {
					if _, err := tx.ExecContext(ctx, s); err != nil {
						return err
					}
				}
				return nil
			},
		},
		flowsMigration(),
		usersMigration(),
		{
			Version: 4,
			Name:    "attempt-next-attempt-at",
			Apply: func(ctx context.Context, tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, `ALTER TABLE message_attempts ADD COLUMN next_attempt_at BIGINT NOT NULL DEFAULT 0`)
				return err
			},
		},
		{
			Version: 5,
			Name:    "messages-by-flow-index",
			Apply: func(ctx context.Context, tx *sql.Tx) error {
				for _, stmt := range []string{
					`CREATE INDEX IF NOT EXISTS messages_flow_received ON messages (flow_id, received_at, id)`,
					`CREATE INDEX IF NOT EXISTS messages_received ON messages (received_at, id)`,
				} {
					if _, err := tx.ExecContext(ctx, stmt); err != nil {
						return err
					}
				}
				return nil
			},
		},
		itemsMigration(),
		lookupsMigration(),
		{
			Version: 8,
			Name:    "attempt-code-and-time",
			Apply: func(ctx context.Context, tx *sql.Tx) error {
				for _, stmt := range []string{
					`ALTER TABLE message_attempts ADD COLUMN last_code TEXT NOT NULL DEFAULT ''`,
					`ALTER TABLE message_attempts ADD COLUMN last_attempt_at BIGINT NOT NULL DEFAULT 0`,
				} {
					if _, err := tx.ExecContext(ctx, stmt); err != nil {
						return err
					}
				}
				return nil
			},
		},
		{
			// The scheduler's durable job queue (was created by the queue, which
			// no server database used; CREATE TABLE refuses a foreign jobs table).
			Version: 9,
			Name:    "jobs",
			Apply: func(ctx context.Context, tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, `CREATE TABLE jobs (
					id TEXT PRIMARY KEY,
					type TEXT NOT NULL,
					payload TEXT NOT NULL DEFAULT '',
					next_run_at BIGINT NOT NULL DEFAULT 0,
					status TEXT NOT NULL DEFAULT 'queued',
					claimed_by TEXT NOT NULL DEFAULT '',
					lease_until BIGINT NOT NULL DEFAULT 0,
					attempts INTEGER NOT NULL DEFAULT 0,
					last_error TEXT NOT NULL DEFAULT ''
				)`)
				return err
			},
		},
		{
			// Search and counts by status (the common filter) without a scan.
			// Metadata values are not indexed: they can be long (errors), and
			// a PostgreSQL btree entry is limited to about 2.7 KB.
			Version: 10,
			Name:    "messages-by-status-index",
			Apply: func(ctx context.Context, tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS messages_status_received ON messages (status, received_at, id)`)
				return err
			},
		},
		{
			// The scheduler claims the earliest due queued job, then by id.
			Version: 11,
			Name:    "jobs-claim-index",
			Apply: func(ctx context.Context, tx *sql.Tx) error {
				// id in byte order, as claims sort it, so PostgreSQL can use it.
				_, err := tx.ExecContext(ctx, forDialect(ctx, `CREATE INDEX IF NOT EXISTS jobs_claim ON jobs (status, next_run_at, id /*C*/)`))
				return err
			},
		},
		{
			// PostgreSQL: message ids in byte order at the column, as searches
			// page and sort them (id /*C*/) and SQLite compares them, so the
			// primary key and every message index (rebuilt by the ALTER)
			// serve those queries. The message_id columns follow, so joins
			// compare within one collation. SQLite: nothing to do.
			Version: 12,
			Name:    "messages-byte-order-ids",
			Apply: func(ctx context.Context, tx *sql.Tx) error {
				if !migratingPostgres(ctx) {
					return nil
				}
				for _, stmt := range []string{
					`ALTER TABLE messages ALTER COLUMN id TYPE TEXT COLLATE "C"`,
					`ALTER TABLE message_metadata ALTER COLUMN message_id TYPE TEXT COLLATE "C"`,
					`ALTER TABLE message_attempts ALTER COLUMN message_id TYPE TEXT COLLATE "C"`,
				} {
					if _, err := tx.ExecContext(ctx, stmt); err != nil {
						return err
					}
				}
				return nil
			},
		},
		{
			// Audit entries (who did what, to what, when), searchable.
			Version: 13,
			Name:    "audit-log",
			Apply: func(ctx context.Context, tx *sql.Tx) error {
				id := `id INTEGER PRIMARY KEY AUTOINCREMENT`
				if migratingPostgres(ctx) {
					id = `id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY`
				}
				for _, stmt := range []string{
					`CREATE TABLE audit_log (` + id + `,
						at BIGINT NOT NULL,
						actor TEXT NOT NULL,
						action TEXT NOT NULL,
						resource TEXT NOT NULL,
						detail TEXT NOT NULL DEFAULT '{}'
					)`,
					`CREATE INDEX audit_log_at ON audit_log (at)`,
					`CREATE INDEX audit_log_actor ON audit_log (actor, id)`,
					`CREATE INDEX audit_log_action ON audit_log (action, id)`,
					`CREATE INDEX audit_log_resource ON audit_log (resource, id)`,
				} {
					if _, err := tx.ExecContext(ctx, stmt); err != nil {
						return err
					}
				}
				return nil
			},
		},
	}
}

// migrationLock is the PostgreSQL advisory lock key Migrate holds.
const migrationLock int64 = 0x7765617673746572 // "weavster"

// NewerSchemaError refuses a database a newer release migrated (Migrate);
// retrying cannot help.
type NewerSchemaError struct {
	Version   int    // the database's schema version
	Supported int    // the newest this release knows
	WrittenBy string // the release that applied Version ("" if not recorded)
}

func (e *NewerSchemaError) Error() string {
	if e.WrittenBy == "" {
		return fmt.Sprintf("state: the database schema is at version %d (written by an unknown weavster release), newer than this release supports (%d): run a newer weavster release, or restore a backup taken before the upgrade",
			e.Version, e.Supported)
	}
	return fmt.Sprintf("state: the database schema is at version %d (written by weavster %s), newer than this release supports (%d): run weavster %s or later, or restore a backup taken before the upgrade",
		e.Version, e.WrittenBy, e.Supported, e.WrittenBy)
}

// AppVersion is the weavster version recorded with each migration it
// applies (schema_migrations.app_version); the binary sets it at start.
var AppVersion = "dev"

// Migrate runs pending forward-only migrations against db, recording the
// applied version and AppVersion in schema_migrations (gap #7). The chain
// must be numbered 1, 2, 3, … in order. A database at a version newer than
// the chain's last is refused unchanged: it was written by a newer release.
//
// Everything runs on one connection, which on PostgreSQL also holds the
// advisory lock, so a pool of a single connection cannot wait for itself.
func Migrate(ctx context.Context, db *sql.DB, migrations []Migration) error {
	for i, m := range migrations {
		if m.Version != i+1 {
			return fmt.Errorf("state: migration %q is number %d at position %d: migrations must be numbered 1, 2, 3, … in order", m.Name, m.Version, i+1)
		}
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	postgres := isPostgres(db)
	if postgres {
		// One server at a time: others wait here, then find the migrations
		// applied (a session advisory lock).
		if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrationLock); err != nil {
			return err
		}
		defer func() {
			_, _ = conn.ExecContext(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrationLock)
		}()
	}
	// Read the version before changing anything, so a newer release's
	// database is refused untouched.
	cols, err := columns(ctx, conn, postgres, "schema_migrations")
	if err != nil {
		return err
	}
	current, writtenBy := 0, ""
	if len(cols) > 0 {
		if current, writtenBy, err = currentVersion(ctx, conn, cols["app_version"]); err != nil {
			return err
		}
	}
	if current > len(migrations) {
		return &NewerSchemaError{Version: current, Supported: len(migrations), WrittenBy: writtenBy}
	}
	switch {
	case len(cols) == 0:
		_, err = conn.ExecContext(ctx,
			`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, app_version TEXT NOT NULL DEFAULT '')`)
	case !cols["app_version"]: // a database from before app_version was recorded
		_, err = conn.ExecContext(ctx, `ALTER TABLE schema_migrations ADD COLUMN app_version TEXT NOT NULL DEFAULT ''`)
	}
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, postgresKey{}, postgres) // forDialect, in migrations
	for _, m := range migrations[current:] {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := m.Apply(ctx, tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("state: migration %d (%s): %w", m.Version, m.Name, err)
		}
		if _, err := tx.ExecContext(ctx,
			rebind(postgres, `INSERT INTO schema_migrations (version, name, app_version) VALUES (?, ?, ?)`), m.Version, m.Name, AppVersion); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// postgresKey marks a migration's context as running on PostgreSQL.
type postgresKey struct{}

// forDialect is a migration statement written for SQLite, rewritten for
// PostgreSQL when the migration runs there (rebind: byte-order marks and
// placeholders).
func forDialect(ctx context.Context, stmt string) string {
	return rebind(migratingPostgres(ctx), stmt)
}

// migratingPostgres reports whether a migration runs on PostgreSQL.
func migratingPostgres(ctx context.Context) bool {
	postgres, _ := ctx.Value(postgresKey{}).(bool)
	return postgres
}

// currentVersion is the database's schema version and, when recorded, the
// weavster release that applied it (0 and "" for an empty table).
func currentVersion(ctx context.Context, conn *sql.Conn, hasAppVersion bool) (int, string, error) {
	q := `SELECT version, '' FROM schema_migrations ORDER BY version DESC LIMIT 1`
	if hasAppVersion {
		q = `SELECT version, app_version FROM schema_migrations ORDER BY version DESC LIMIT 1`
	}
	var v int
	var app string
	err := conn.QueryRowContext(ctx, q).Scan(&v, &app)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", nil
	}
	return v, app, err
}

// columns names the columns of table as an unqualified name resolves it
// (on PostgreSQL through the whole search_path), none when there is none.
func columns(ctx context.Context, conn *sql.Conn, postgres bool, table string) (map[string]bool, error) {
	q := `SELECT name FROM pragma_table_info(?)`
	if postgres {
		q = `SELECT attname FROM pg_attribute WHERE attrelid = to_regclass($1::text) AND attnum > 0 AND NOT attisdropped`
	}
	rows, err := conn.QueryContext(ctx, q, table)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	cols := map[string]bool{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		cols[c] = true
	}
	return cols, rows.Err()
}

// sortMessages sorts messages by q.Sort.
func sortMessages(ms []Message, sortBy string) {
	asc := !strings.HasPrefix(sortBy, "-")
	field := strings.TrimPrefix(sortBy, "-")
	if field == "" {
		field = "id"
	}
	// A strict order with the id as tie-breaker, as the SQL store sorts.
	sort.Slice(ms, func(i, j int) bool {
		a, b := ms[i], ms[j]
		if !asc {
			a, b = b, a
		}
		if field == "received_at" && !a.ReceivedAt.Equal(b.ReceivedAt) {
			return a.ReceivedAt.Before(b.ReceivedAt)
		}
		return a.ID < b.ID
	})
}
