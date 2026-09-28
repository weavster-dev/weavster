package state

import (
	"context"
	"database/sql"
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
	}
}

// Migrate runs pending forward-only migrations against db, recording the
// applied version in schema_migrations (gap #7).
func Migrate(ctx context.Context, db *sql.DB, migrations []Migration) error {
	if _, err := db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL)`); err != nil {
		return err
	}

	current := currentVersion(ctx, db)
	for _, m := range migrations {
		if m.Version <= current {
			continue // forward-only: never downgrade or re-apply
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := m.Apply(ctx, tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("state: migration %d (%s): %w", m.Version, m.Name, err)
		}
		if _, err := tx.ExecContext(ctx,
			rebind(isPostgres(db), `INSERT INTO schema_migrations (version, name) VALUES (?, ?)`), m.Version, m.Name); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func currentVersion(ctx context.Context, db *sql.DB) int {
	var v int
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&v)
	return v
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
