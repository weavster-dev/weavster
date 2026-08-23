package adapters

import (
	"context"
	"database/sql"
	"io"
)

// DBSink writes messages as rows into a database table.
type DBSink struct {
	db    *sql.DB
	table string
}

// NewDBSink returns a database sink writing to table (created if absent).
func NewDBSink(db *sql.DB, table string) (*DBSink, error) {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS ` + table + ` (id TEXT PRIMARY KEY, body BLOB)`); err != nil {
		return nil, err
	}
	return &DBSink{db: db, table: table}, nil
}

func (s *DBSink) Name() string { return "database" }

func (s *DBSink) Write(ctx context.Context, m Message) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO `+s.table+` (id, body) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET body = excluded.body`,
		m.ID, m.Body)
	return err
}

func (s *DBSink) Close() error { return nil }

// DBSource reads messages from a database query.
type DBSource struct {
	db    *sql.DB
	query string
	rows  *sql.Rows
}

// NewDBSource returns a source running query (columns: id, body).
func NewDBSource(db *sql.DB, query string) *DBSource {
	return &DBSource{db: db, query: query}
}

func (s *DBSource) Name() string { return "database" }

func (s *DBSource) Read(ctx context.Context) (Message, error) {
	if s.rows == nil {
		rows, err := s.db.QueryContext(ctx, s.query)
		if err != nil {
			return Message{}, err
		}
		s.rows = rows
	}
	if !s.rows.Next() {
		if err := s.rows.Err(); err != nil {
			return Message{}, err
		}
		return Message{}, io.EOF
	}
	var m Message
	if err := s.rows.Scan(&m.ID, &m.Body); err != nil {
		return Message{}, err
	}
	return m, nil
}

func (s *DBSource) Close() error {
	if s.rows != nil {
		return s.rows.Close()
	}
	return nil
}
