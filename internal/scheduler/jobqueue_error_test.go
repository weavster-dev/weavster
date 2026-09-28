package scheduler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

// SQL for the lease-lifecycle operations. These are single-line so they match
// the normalized matcher installed by newPostgresMockQueue verbatim.
const (
	createJobsTable   = `CREATE TABLE IF NOT EXISTS jobs`
	claimSQLiteUpdate = `UPDATE jobs SET claimed_by = ?, lease_until = ?, status = 'running', attempts = attempts + 1
		WHERE id = (
			SELECT id FROM jobs
			WHERE status = 'queued' AND next_run_at <= ? AND lease_until <= ?
			ORDER BY next_run_at ASC LIMIT 1
		)
		RETURNING id, type, payload`
	heartbeatUpdate = `UPDATE jobs SET lease_until = ? WHERE id = ? AND claimed_by = ?`
	completeDelete  = `DELETE FROM jobs WHERE id = ? AND claimed_by = ?`
	requeueUpdate   = `UPDATE jobs SET status = 'queued', claimed_by = '', lease_until = 0, last_error = ? WHERE id = ? AND claimed_by = ?`
	reconcileUpdate = `UPDATE jobs SET status = 'queued', claimed_by = '' WHERE status = 'running' AND lease_until <= ?`
)

func TestNewSQLJobQueueDBError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	wantErr := errors.New("create jobs table failed")
	mock.ExpectExec(createJobsTable).WillReturnError(wantErr)

	q, err := NewSQLJobQueue(db, "sqlite")
	if q != nil {
		t.Errorf("NewSQLJobQueue() queue = %v, want nil", q)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("NewSQLJobQueue() error = %v, want %v", err, wantErr)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSQLJobQueueClaimRoutesPostgresError(t *testing.T) {
	q, mock := newPostgresMockQueue(t)
	wantErr := errors.New("begin claim transaction failed")
	mock.ExpectBegin().WillReturnError(wantErr)

	job, ok, err := q.Claim(context.Background(), "node-a", time.Minute)
	if job != (Job{}) {
		t.Errorf("Claim() job = %+v, want zero value", job)
	}
	if ok {
		t.Error("Claim() ok = true, want false")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("Claim() error = %v, want %v", err, wantErr)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSQLJobQueueClaimSQLiteQueryError(t *testing.T) {
	q, mock := newPostgresMockQueue(t)
	q.dialect = "sqlite"
	wantErr := errors.New("claim update failed")
	mock.ExpectQuery(claimSQLiteUpdate).
		WithArgs("node-a", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(wantErr)

	job, ok, err := q.Claim(context.Background(), "node-a", time.Minute)
	if job != (Job{}) {
		t.Errorf("Claim() job = %+v, want zero value", job)
	}
	if ok {
		t.Error("Claim() ok = true, want false")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("Claim() error = %v, want %v", err, wantErr)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestSQLJobQueueHeartbeatDBError covers the ExecContext error branch of
// SQLJobQueue.Heartbeat (line 263): when the lease extension UPDATE fails at
// the database layer the error must propagate to the caller.
func TestSQLJobQueueHeartbeatDBError(t *testing.T) {
	q, mock := newPostgresMockQueue(t)
	ctx := context.Background()

	mock.ExpectExec(heartbeatUpdate).
		WithArgs(sqlmock.AnyArg(), "j1", "node-a").
		WillReturnError(errors.New("connection lost"))

	if err := q.Heartbeat(ctx, "j1", "node-a", time.Minute); err == nil {
		t.Fatal("Heartbeat: expected DB error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestSQLJobQueueHeartbeatRowsAffectedError covers requireAffected's
// RowsAffected error branch: a successful statement whose result cannot be
// inspected still surfaces an error.
func TestSQLJobQueueHeartbeatRowsAffectedError(t *testing.T) {
	q, mock := newPostgresMockQueue(t)
	ctx := context.Background()

	mock.ExpectExec(heartbeatUpdate).
		WithArgs(sqlmock.AnyArg(), "j1", "node-a").
		WillReturnResult(sqlmock.NewErrorResult(errors.New("rows affected boom")))

	if err := q.Heartbeat(ctx, "j1", "node-a", time.Minute); err == nil {
		t.Fatal("Heartbeat: expected RowsAffected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestSQLJobQueueCompleteDBError covers the ExecContext error branch of
// SQLJobQueue.Complete (line 271).
func TestSQLJobQueueCompleteDBError(t *testing.T) {
	q, mock := newPostgresMockQueue(t)
	ctx := context.Background()

	mock.ExpectExec(completeDelete).
		WithArgs("j1", "node-a").
		WillReturnError(errors.New("delete failed"))

	if err := q.Complete(ctx, "j1", "node-a"); err == nil {
		t.Fatal("Complete: expected DB error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestSQLJobQueueCompleteRowsAffectedError covers the RowsAffected error
// branch of SQLJobQueue.Complete via requireAffected.
func TestSQLJobQueueCompleteRowsAffectedError(t *testing.T) {
	q, mock := newPostgresMockQueue(t)
	ctx := context.Background()

	mock.ExpectExec(completeDelete).
		WithArgs("j1", "node-a").
		WillReturnResult(sqlmock.NewErrorResult(errors.New("rows affected boom")))

	if err := q.Complete(ctx, "j1", "node-a"); err == nil {
		t.Fatal("Complete: expected RowsAffected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestSQLJobQueueRequeueDBError covers the ExecContext error branch of
// SQLJobQueue.Requeue (line 281).
func TestSQLJobQueueRequeueDBError(t *testing.T) {
	q, mock := newPostgresMockQueue(t)
	ctx := context.Background()

	mock.ExpectExec(requeueUpdate).
		WithArgs("boom", "j1", "node-a").
		WillReturnError(errors.New("update failed"))

	if err := q.Requeue(ctx, "j1", "node-a", "boom"); err == nil {
		t.Fatal("Requeue: expected DB error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestSQLJobQueueRequeueRowsAffectedError covers the RowsAffected error
// branch of SQLJobQueue.Requeue via requireAffected.
func TestSQLJobQueueRequeueRowsAffectedError(t *testing.T) {
	q, mock := newPostgresMockQueue(t)
	ctx := context.Background()

	mock.ExpectExec(requeueUpdate).
		WithArgs("boom", "j1", "node-a").
		WillReturnResult(sqlmock.NewErrorResult(errors.New("rows affected boom")))

	if err := q.Requeue(ctx, "j1", "node-a", "boom"); err == nil {
		t.Fatal("Requeue: expected RowsAffected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSQLJobQueueReconcileDBError(t *testing.T) {
	q, mock := newPostgresMockQueue(t)

	mock.ExpectExec(reconcileUpdate).
		WithArgs(sqlmock.AnyArg()).
		WillReturnError(errors.New("update failed"))

	if _, err := q.Reconcile(context.Background(), "node-a"); err == nil {
		t.Fatal("Reconcile: expected DB error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// errRowsResult is a sql.Result whose RowsAffected always fails, used to unit
// test requireAffected in isolation.
type errRowsResult struct{ err error }

func (r errRowsResult) LastInsertId() (int64, error) { return 0, r.err }
func (r errRowsResult) RowsAffected() (int64, error) { return 0, r.err }

// TestRequireAffected covers the three branches of requireAffected: success,
// zero-rows-affected (stale ownership), and a RowsAffected failure.
func TestRequireAffected(t *testing.T) {
	if err := requireAffected(sqlmock.NewResult(0, 1)); err != nil {
		t.Errorf("requireAffected(1 row) = %v, want nil", err)
	}

	err := requireAffected(sqlmock.NewResult(0, 0))
	if err == nil {
		t.Fatal("requireAffected(0 rows) = nil, want ownership error")
	}
	if !strings.Contains(err.Error(), "job not owned by this node") {
		t.Errorf("requireAffected(0 rows) = %q, want ownership error", err)
	}

	if err := requireAffected(errRowsResult{err: errors.New("boom")}); err == nil {
		t.Fatal("requireAffected(RowsAffected error) = nil, want error")
	}
}
