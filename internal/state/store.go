// Package state implements the Store port with Postgres, SQLite, and in-memory
// backends plus migrations, search, and export.
package state

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"
)

// ErrNotFound is returned when a message id does not exist.
var ErrNotFound = errors.New("state: message not found")

// Status is a message lifecycle state (spec §6.2).
type Status string

const (
	StatusReceived    Status = "received"
	StatusFiltered    Status = "filtered"
	StatusTransformed Status = "transformed"
	StatusSent        Status = "sent"
	StatusQueued      Status = "queued"
	StatusErrored     Status = "errored"
	// StatusDeadLettered: a destination exhausted its retry budget.
	StatusDeadLettered Status = "dead-lettered"
)

// DestinationAttempt tracks per-(message,destination) send attempts and the
// last error code (spec §2.5.14, §6.3).
type DestinationAttempt struct {
	Attempts  int    `json:"attempts"`
	LastError string `json:"lastError"`
	// LastCode is the last failure's protocol-specific code ("http:503",
	// "mllp:AE", "sqlstate:23505", "net:timeout", …; "" for none or after a
	// success), and LastAttemptAt when the last attempt ended (#107 D-78).
	LastCode      string    `json:"lastCode,omitempty"`
	LastAttemptAt time.Time `json:"lastAttemptAt,omitempty"`
	// NextAttemptAt is when a failed delivery is due for retry.
	NextAttemptAt time.Time `json:"nextAttemptAt,omitempty"`
}

// Message is a persisted message with its content forms and metadata
// (spec §2.6.17).
type Message struct {
	ID          string                        `json:"id"`
	FlowID      string                        `json:"flowId"`
	Status      Status                        `json:"status"`
	ContentType string                        `json:"contentType"`
	ReceivedAt  time.Time                     `json:"receivedAt"`
	UpdatedAt   time.Time                     `json:"updatedAt"`
	Raw         []byte                        `json:"-"`
	Processed   []byte                        `json:"-"`
	Transformed []byte                        `json:"-"`
	Encoded     []byte                        `json:"-"`
	Response    []byte                        `json:"-"`
	Original    []byte                        `json:"-"`
	Metadata    map[string]string             `json:"metadata,omitempty"`
	Attempts    map[string]DestinationAttempt `json:"attempts,omitempty"`
}

// Query narrows a message search (spec §2.6.18).
type Query struct {
	IDFrom string
	IDTo   string
	// IDAfter selects ids after it (exclusive): the cursor of a page read
	// in id order.
	IDAfter     string
	FlowID      string
	From        time.Time
	To          time.Time
	Status      Status
	ContentType string
	MinAttempts int
	MaxAttempts int
	Metadata    map[string]string
	Limit       int
	Offset      int
	Sort        string // "id", "received_at", or "-" prefix for descending
}

// Store is the port for durable state: received/intermediate/sent messages
// with search and export (arch §3.1).
type Store interface {
	Put(ctx context.Context, m Message) error
	Get(ctx context.Context, id string) (Message, error)
	Delete(ctx context.Context, id string) error
	Search(ctx context.Context, q Query) ([]Message, error)
	// MessageTrends counts messages per time bucket and status.
	MessageTrends(ctx context.Context, q TrendQuery) (TrendCounts, error)
	Close() error
}

// sqlStore is the shared SQL-backed Store core used by the SQLite and
// Postgres adapters (schema and query semantics are identical).
type sqlStore struct {
	db *dialectDB
	// uncancelable detaches caller cancellation from every statement.
	// SQLite sets it: modernc.org/sqlite (up to at least v1.38) leaves the
	// file open and locked after Close when a context cancels a statement,
	// so the next open of the same file fails with SQLITE_BUSY. SQLite
	// statements are local and short, so they simply run to completion.
	uncancelable bool
}

// bind returns the context statements run with.
func (s *sqlStore) bind(ctx context.Context) context.Context {
	if s.uncancelable {
		return context.WithoutCancel(ctx)
	}
	return ctx
}

func openSQLStore(ctx context.Context, db *sql.DB, postgres bool) (*sqlStore, error) {
	s := &sqlStore{db: &dialectDB{db: db, postgres: postgres}}
	if err := Migrate(ctx, db, Migrations()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *sqlStore) Close() error { return s.db.Close() }

// storableText is m with its metadata and attempt texts as every backend
// stores them (textValue: no NUL bytes, valid UTF-8).
func storableText(m Message) Message {
	if len(m.Metadata) > 0 {
		md := make(map[string]string, len(m.Metadata))
		for _, k := range storableKeys(m.Metadata) {
			if _, taken := md[textValue(k)]; !taken {
				md[textValue(k)] = textValue(m.Metadata[k])
			}
		}
		m.Metadata = md
	}
	if len(m.Attempts) > 0 {
		at := make(map[string]DestinationAttempt, len(m.Attempts))
		for _, d := range storableKeys(m.Attempts) {
			if _, taken := at[textValue(d)]; !taken {
				a := m.Attempts[d]
				a.LastError, a.LastCode = textValue(a.LastError), textValue(a.LastCode)
				at[textValue(d)] = a
			}
		}
		m.Attempts = at
	}
	return m
}

// storableKeys orders a map's keys so that when two normalise to the same
// text the survivor is always the same one: a key stored as given wins over
// one that normalisation changed, and otherwise the bytewise-lower key wins.
func storableKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		ci, cj := textValue(keys[i]) != keys[i], textValue(keys[j]) != keys[j]
		if ci != cj {
			return cj
		}
		return keys[i] < keys[j]
	})
	return keys
}

func (s *sqlStore) Put(ctx context.Context, m Message) error {
	ctx = s.bind(ctx)
	m = storableText(m)
	now := time.Now()
	if m.ReceivedAt.IsZero() {
		m.ReceivedAt = now
	}
	m.UpdatedAt = now

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO messages (id, flow_id, status, content_type, received_at, updated_at,
			raw, processed, transformed, encoded, response, original)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			flow_id=excluded.flow_id, status=excluded.status, content_type=excluded.content_type,
			updated_at=excluded.updated_at, raw=excluded.raw, processed=excluded.processed,
			transformed=excluded.transformed, encoded=excluded.encoded, response=excluded.response,
			original=excluded.original`,
		m.ID, m.FlowID, string(m.Status), m.ContentType,
		m.ReceivedAt.UnixMilli(), m.UpdatedAt.UnixMilli(),
		m.Raw, m.Processed, m.Transformed, m.Encoded, m.Response, m.Original)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM message_metadata WHERE message_id = ?`, m.ID); err != nil {
		return err
	}
	for k, v := range m.Metadata {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO message_metadata (message_id, key, value) VALUES (?, ?, ?)`, m.ID, k, v); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM message_attempts WHERE message_id = ?`, m.ID); err != nil {
		return err
	}
	for dest, a := range m.Attempts {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO message_attempts (message_id, destination, attempts, last_error, next_attempt_at, last_code, last_attempt_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			m.ID, dest, a.Attempts, a.LastError, unixMilli(a.NextAttemptAt), a.LastCode, unixMilli(a.LastAttemptAt)); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *sqlStore) Get(ctx context.Context, id string) (Message, error) {
	ctx = s.bind(ctx)
	row := s.db.QueryRowContext(ctx, `
		SELECT id, flow_id, status, content_type, received_at, updated_at,
			raw, processed, transformed, encoded, response, original
		FROM messages WHERE id = ?`, id)
	var m Message
	var status string
	var recvAt, updAt int64
	if err := row.Scan(&m.ID, &m.FlowID, &status, &m.ContentType, &recvAt, &updAt,
		&m.Raw, &m.Processed, &m.Transformed, &m.Encoded, &m.Response, &m.Original); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Message{}, ErrNotFound
		}
		return Message{}, err
	}
	m.Status = Status(status)
	m.ReceivedAt = time.UnixMilli(recvAt)
	m.UpdatedAt = time.UnixMilli(updAt)

	var err error
	if m.Metadata, err = s.loadMetadata(ctx, id); err != nil {
		return Message{}, err
	}
	if m.Attempts, err = s.loadAttempts(ctx, id); err != nil {
		return Message{}, err
	}
	return m, nil
}

func (s *sqlStore) Delete(ctx context.Context, id string) error {
	ctx = s.bind(ctx)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// The message row first, as Put writes it first: both lock in the same
	// order, so a Put and a Delete of one message cannot deadlock.
	for _, q := range []string{
		`DELETE FROM messages WHERE id = ?`,
		`DELETE FROM message_metadata WHERE message_id = ?`,
		`DELETE FROM message_attempts WHERE message_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *sqlStore) Search(ctx context.Context, q Query) ([]Message, error) {
	ctx = s.bind(ctx)
	where, args := buildWhere(q)
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	query := `SELECT id FROM messages ` + where + ` ` + buildOrderSort(q.Sort) + ` LIMIT ? OFFSET ?`
	args = append(args, limit, q.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]Message, 0, len(ids))
	for _, id := range ids {
		m, err := s.Get(ctx, id)
		if errors.Is(err, ErrNotFound) {
			continue // deleted since the id query
		}
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func (s *sqlStore) loadMetadata(ctx context.Context, id string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM message_metadata WHERE message_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (s *sqlStore) loadAttempts(ctx context.Context, id string) (map[string]DestinationAttempt, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT destination, attempts, last_error, next_attempt_at, last_code, last_attempt_at FROM message_attempts WHERE message_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make(map[string]DestinationAttempt)
	for rows.Next() {
		var dest string
		var a DestinationAttempt
		var next, last int64
		if err := rows.Scan(&dest, &a.Attempts, &a.LastError, &next, &a.LastCode, &last); err != nil {
			return nil, err
		}
		if next != 0 {
			a.NextAttemptAt = time.UnixMilli(next)
		}
		if last != 0 {
			a.LastAttemptAt = time.UnixMilli(last)
		}
		out[dest] = a
	}
	return out, rows.Err()
}

// unixMilli stores the zero time as 0.
func unixMilli(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

var _ Store = (*sqlStore)(nil)
