package adapters

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

// newMockDB returns a *sql.DB backed by go-sqlmock so the database adapter's
// error paths can be exercised without a live backend. The default regexp
// matcher is used, so expectations quote their SQL with regexp.QuoteMeta.
func newMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, mock
}

// TestNewDBSinkExecError covers the CREATE TABLE failure branch of
// NewDBSink: a backend that rejects the schema DDL must surface the error
// and return a nil sink.
func TestNewDBSinkExecError(t *testing.T) {
	db, mock := newMockDB(t)

	mock.ExpectExec(regexp.QuoteMeta(`CREATE TABLE IF NOT EXISTS messages (id TEXT PRIMARY KEY, body BLOB)`)).
		WillReturnError(errors.New("db: create table failed"))

	sink, err := NewDBSink(db, "messages")
	if err == nil {
		t.Fatal("NewDBSink: expected error, got nil")
	}
	if sink != nil {
		t.Errorf("NewDBSink: expected nil sink on error, got %+v", sink)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestDBSourceReadQueryError covers the QueryContext failure branch of
// DBSource.Read: a backend that rejects the SELECT must surface the error
// instead of hanging or returning a bogus message.
func TestDBSourceReadQueryError(t *testing.T) {
	db, mock := newMockDB(t)
	const q = "SELECT id, body FROM messages ORDER BY id"

	mock.ExpectQuery(regexp.QuoteMeta(q)).
		WillReturnError(errors.New("db: query failed"))

	src := NewDBSource(db, q)
	if _, err := src.Read(context.Background()); err == nil {
		t.Fatal("Read: expected query error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestDBSourceReadRowsErr covers the rows.Err() branch of DBSource.Read: when
// the driver reports an error while iterating (after Next returns false), the
// error must be surfaced rather than misinterpreted as io.EOF.
func TestDBSourceReadRowsErr(t *testing.T) {
	db, mock := newMockDB(t)
	const q = "SELECT id, body FROM messages ORDER BY id"

	rows := sqlmock.NewRows([]string{"id", "body"}).
		AddRow("m1", []byte("b1")).
		RowError(0, errors.New("db: row iteration failed"))
	mock.ExpectQuery(regexp.QuoteMeta(q)).WillReturnRows(rows)

	src := NewDBSource(db, q)
	if _, err := src.Read(context.Background()); err == nil {
		t.Fatal("Read: expected row iteration error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestDBSourceReadScanError covers the rows.Scan failure branch of
// DBSource.Read: a row whose column types cannot be scanned into the Message
// fields must surface the conversion error.
func TestDBSourceReadScanError(t *testing.T) {
	db, mock := newMockDB(t)
	const q = "SELECT id, body FROM messages ORDER BY id"

	// A single-column result set cannot satisfy the two Scan destinations
	// (id, body), forcing rows.Scan to return a column-count error.
	rows := sqlmock.NewRows([]string{"id"}).AddRow("m1")
	mock.ExpectQuery(regexp.QuoteMeta(q)).WillReturnRows(rows)

	src := NewDBSource(db, q)
	if _, err := src.Read(context.Background()); err == nil {
		t.Fatal("Read: expected scan error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
