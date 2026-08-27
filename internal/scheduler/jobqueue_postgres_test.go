package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

// newPostgresMockQueue builds a SQLJobQueue whose *sql.DB is backed by
// go-sqlmock so the Postgres-only claim path (FOR UPDATE SKIP LOCKED) can be
// exercised without a live database. Queries are matched after whitespace
// normalization so the multi-line SQL in jobqueue.go stays readable.
func newPostgresMockQueue(t *testing.T) (*SQLJobQueue, sqlmock.Sqlmock) {
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

	q := &SQLJobQueue{db: db, dialect: "postgres"}
	return q, mock
}

const claimSelect = `SELECT id, type, payload FROM jobs
		WHERE status = 'queued' AND next_run_at <= $1 AND lease_until <= $1
		ORDER BY next_run_at ASC LIMIT 1
		FOR UPDATE SKIP LOCKED`

const claimUpdate = `UPDATE jobs SET claimed_by = $1, lease_until = $2, status = 'running', attempts = attempts + 1 WHERE id = $3`

func TestClaimPostgresHappyPath(t *testing.T) {
	q, mock := newPostgresMockQueue(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()
	leaseUntil := time.Now().Add(time.Minute).UnixMilli()

	mock.ExpectBegin()
	mock.ExpectQuery(claimSelect).
		WithArgs(now).
		WillReturnRows(sqlmock.NewRows([]string{"id", "type", "payload"}).
			AddRow("j1", "poll", "{}"))
	mock.ExpectExec(claimUpdate).
		WithArgs("node-a", leaseUntil, "j1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	job, ok, err := q.claimPostgres(ctx, "node-a", now, leaseUntil)
	if err != nil {
		t.Fatalf("claimPostgres: %v", err)
	}
	if !ok {
		t.Fatal("claimPostgres: ok = false, want true")
	}
	if job.ID != "j1" || job.Type != "poll" || job.Payload != "{}" {
		t.Errorf("job = %+v, want id=j1 type=poll payload={}", job)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestClaimPostgresNoDueRows(t *testing.T) {
	q, mock := newPostgresMockQueue(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	mock.ExpectBegin()
	mock.ExpectQuery(claimSelect).
		WithArgs(now).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	job, ok, err := q.claimPostgres(ctx, "node-a", now, now+1)
	if err != nil {
		t.Fatalf("claimPostgres: %v", err)
	}
	if ok {
		t.Errorf("claimPostgres: ok = true, want false (no due rows)")
	}
	if job.ID != "" {
		t.Errorf("job = %+v, want zero-value Job", job)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestClaimPostgresBeginTxError(t *testing.T) {
	q, mock := newPostgresMockQueue(t)
	ctx := context.Background()

	mock.ExpectBegin().WillReturnError(errors.New("connect refused"))

	_, ok, err := q.claimPostgres(ctx, "node-a", 1, 2)
	if err == nil {
		t.Fatal("claimPostgres: expected BeginTx error, got nil")
	}
	if ok {
		t.Error("claimPostgres: ok = true, want false on error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestClaimPostgresScanError(t *testing.T) {
	q, mock := newPostgresMockQueue(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	mock.ExpectBegin()
	mock.ExpectQuery(claimSelect).
		WithArgs(now).
		WillReturnError(errors.New("scan boom"))
	mock.ExpectRollback()

	_, ok, err := q.claimPostgres(ctx, "node-a", now, now+1)
	if err == nil {
		t.Fatal("claimPostgres: expected scan error, got nil")
	}
	if ok {
		t.Error("claimPostgres: ok = true, want false on error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestClaimPostgresUpdateError(t *testing.T) {
	q, mock := newPostgresMockQueue(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()
	leaseUntil := time.Now().Add(time.Minute).UnixMilli()

	mock.ExpectBegin()
	mock.ExpectQuery(claimSelect).
		WithArgs(now).
		WillReturnRows(sqlmock.NewRows([]string{"id", "type", "payload"}).
			AddRow("j1", "poll", "{}"))
	mock.ExpectExec(claimUpdate).
		WithArgs("node-a", leaseUntil, "j1").
		WillReturnError(errors.New("update boom"))
	mock.ExpectRollback()

	_, ok, err := q.claimPostgres(ctx, "node-a", now, leaseUntil)
	if err == nil {
		t.Fatal("claimPostgres: expected update error, got nil")
	}
	if ok {
		t.Error("claimPostgres: ok = true, want false on error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
