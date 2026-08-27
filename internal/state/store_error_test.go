package state

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

// newMockSQLStore builds a sqlStore whose *sql.DB is backed by go-sqlmock so
// the database-failure branches of Put/Get/Delete can be exercised without a
// live backend. Queries are matched after whitespace normalization so the
// multi-line SQL in store.go stays readable.
func newMockSQLStore(t *testing.T) (*sqlStore, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(
		func(expected, actual string) error {
			norm := func(s string) string { return strings.Join(strings.Fields(s), " ") }
			if norm(expected) != norm(actual) {
				return fmt.Errorf("query mismatch:\nwant: %s\ngot:  %s", norm(expected), norm(actual))
			}
			return nil
		},
	)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &sqlStore{db: db}, mock
}

// Normalized single-line forms of the SQL in store.go (used verbatim by the
// whitespace-normalizing matcher).
const (
	putInsertSQL      = `INSERT INTO messages (id, flow_id, status, content_type, received_at, updated_at, raw, processed, transformed, encoded, response, original) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET flow_id=excluded.flow_id, status=excluded.status, content_type=excluded.content_type, updated_at=excluded.updated_at, raw=excluded.raw, processed=excluded.processed, transformed=excluded.transformed, encoded=excluded.encoded, response=excluded.response, original=excluded.original`
	metadataDeleteSQL = `DELETE FROM message_metadata WHERE message_id = ?`
	attemptsDeleteSQL = `DELETE FROM message_attempts WHERE message_id = ?`
	getMessageSQL     = `SELECT id, flow_id, status, content_type, received_at, updated_at, raw, processed, transformed, encoded, response, original FROM messages WHERE id = ?`
)

func minimalMessage() Message {
	return Message{
		ID:          "m1",
		FlowID:      "flow:a",
		Status:      StatusReceived,
		ContentType: "text/plain",
		ReceivedAt:  time.Now(),
	}
}

func TestSQLStorePutBeginTxError(t *testing.T) {
	s, mock := newMockSQLStore(t)
	ctx := context.Background()

	mock.ExpectBegin().WillReturnError(errors.New("connection refused"))

	if err := s.Put(ctx, minimalMessage()); err == nil {
		t.Fatal("Put: expected BeginTx error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSQLStorePutInsertError(t *testing.T) {
	s, mock := newMockSQLStore(t)
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec(putInsertSQL).
		WillReturnError(errors.New("insert boom"))
	mock.ExpectRollback()

	if err := s.Put(ctx, minimalMessage()); err == nil {
		t.Fatal("Put: expected INSERT error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSQLStorePutCommitError(t *testing.T) {
	s, mock := newMockSQLStore(t)
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec(putInsertSQL).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(metadataDeleteSQL).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(attemptsDeleteSQL).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit().WillReturnError(errors.New("commit boom"))

	if err := s.Put(ctx, minimalMessage()); err == nil {
		t.Fatal("Put: expected Commit error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSQLStoreDeleteBeginTxError(t *testing.T) {
	s, mock := newMockSQLStore(t)
	ctx := context.Background()

	mock.ExpectBegin().WillReturnError(errors.New("connection refused"))

	if err := s.Delete(ctx, "m1"); err == nil {
		t.Fatal("Delete: expected BeginTx error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSQLStoreDeleteExecError(t *testing.T) {
	s, mock := newMockSQLStore(t)
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec(metadataDeleteSQL).
		WithArgs("m1").
		WillReturnError(errors.New("delete boom"))
	mock.ExpectRollback()

	if err := s.Delete(ctx, "m1"); err == nil {
		t.Fatal("Delete: expected ExecContext error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSQLStoreGetScanError(t *testing.T) {
	s, mock := newMockSQLStore(t)
	ctx := context.Background()

	mock.ExpectQuery(getMessageSQL).
		WithArgs("m1").
		WillReturnError(errors.New("scan boom"))

	if _, err := s.Get(ctx, "m1"); err == nil {
		t.Fatal("Get: expected scan error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
