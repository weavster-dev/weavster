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
						received_at INTEGER NOT NULL,
						updated_at INTEGER NOT NULL,
						raw BLOB, processed BLOB, transformed BLOB,
						encoded BLOB, response BLOB, original BLOB
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
			`INSERT INTO schema_migrations (version, name) VALUES (?, ?)`, m.Version, m.Name); err != nil {
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
	sort.Slice(ms, func(i, j int) bool {
		var less bool
		switch field {
		case "received_at":
			less = ms[i].ReceivedAt.Before(ms[j].ReceivedAt)
		case "id":
			less = ms[i].ID < ms[j].ID
		default:
			less = ms[i].ID < ms[j].ID
		}
		if !asc {
			return !less
		}
		return less
	})
}
