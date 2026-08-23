// Package scheduler implements the JobQueue port and interval/cron scheduling
// with durable job claims and lease recovery.
package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"
)

// JobStatus is a durable job state.
type JobStatus string

const (
	StatusQueued  JobStatus = "queued"
	StatusRunning JobStatus = "running"
)

// Job is a unit of scheduler work (poll/transform trigger).
type Job struct {
	ID        string
	Type      string
	Payload   string
	NextRunAt time.Time
}

// JobQueue is the port for durable job claiming (arch §3, §9.1). Claiming is
// atomic: any executor takes a visible lock so a stale node cannot double-claim.
type JobQueue interface {
	Enqueue(ctx context.Context, j Job) error
	// Claim atomically claims the next due job, returning ok=false when none.
	Claim(ctx context.Context, nodeID string, lease time.Duration) (Job, bool, error)
	Heartbeat(ctx context.Context, id, nodeID string, lease time.Duration) error
	Complete(ctx context.Context, id, nodeID string) error
	Requeue(ctx context.Context, id, nodeID, errMsg string) error
	// Reconcile re-queues running jobs whose lease expired (crash recovery).
	Reconcile(ctx context.Context, nodeID string) (int, error)
}

// memJob is the internal in-memory job record.
type memJob struct {
	job        Job
	status     JobStatus
	claimedBy  string
	leaseUntil time.Time
	attempts   int
	lastError  string
}

// MemJobQueue is an in-memory JobQueue (local DX + tests).
type MemJobQueue struct {
	mu   sync.Mutex
	jobs []*memJob
}

// NewMemJobQueue returns an empty in-memory job queue.
func NewMemJobQueue() *MemJobQueue { return &MemJobQueue{} }

func (q *MemJobQueue) Enqueue(_ context.Context, j Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.jobs = append(q.jobs, &memJob{job: j, status: StatusQueued})
	return nil
}

func (q *MemJobQueue) Claim(_ context.Context, nodeID string, lease time.Duration) (Job, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := time.Now()
	for _, m := range q.jobs {
		if m.status != StatusQueued {
			continue
		}
		if !m.job.NextRunAt.IsZero() && m.job.NextRunAt.After(now) {
			continue
		}
		m.status = StatusRunning
		m.claimedBy = nodeID
		m.leaseUntil = now.Add(lease)
		m.attempts++
		return m.job, true, nil
	}
	return Job{}, false, nil
}

func (q *MemJobQueue) Heartbeat(_ context.Context, id, nodeID string, lease time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	m := q.find(id)
	if m == nil {
		return fmt.Errorf("scheduler: job %s not found", id)
	}
	if m.claimedBy != nodeID {
		return fmt.Errorf("scheduler: job %s owned by %s, not %s", id, m.claimedBy, nodeID)
	}
	m.leaseUntil = time.Now().Add(lease)
	return nil
}

func (q *MemJobQueue) Complete(_ context.Context, id, nodeID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	m := q.find(id)
	if m == nil {
		return fmt.Errorf("scheduler: job %s not found", id)
	}
	if m.claimedBy != nodeID {
		return fmt.Errorf("scheduler: job %s owned by %s, not %s", id, m.claimedBy, nodeID)
	}
	q.remove(id)
	return nil
}

func (q *MemJobQueue) Requeue(_ context.Context, id, nodeID, errMsg string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	m := q.find(id)
	if m == nil {
		return fmt.Errorf("scheduler: job %s not found", id)
	}
	if m.claimedBy != nodeID {
		return fmt.Errorf("scheduler: job %s owned by %s, not %s", id, m.claimedBy, nodeID)
	}
	m.status = StatusQueued
	m.claimedBy = ""
	m.leaseUntil = time.Time{}
	m.lastError = errMsg
	return nil
}

func (q *MemJobQueue) Reconcile(_ context.Context, _ string) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := time.Now()
	count := 0
	for _, m := range q.jobs {
		if m.status == StatusRunning && m.leaseUntil.Before(now) {
			m.status = StatusQueued
			m.claimedBy = ""
			count++
		}
	}
	return count, nil
}

func (q *MemJobQueue) find(id string) *memJob {
	for _, m := range q.jobs {
		if m.job.ID == id {
			return m
		}
	}
	return nil
}

func (q *MemJobQueue) remove(id string) {
	out := q.jobs[:0]
	for _, m := range q.jobs {
		if m.job.ID != id {
			out = append(out, m)
		}
	}
	q.jobs = out
}

// SQLJobQueue is a durable JobQueue over database/sql. The "postgres" dialect
// claims with FOR UPDATE SKIP LOCKED; the "sqlite" dialect uses an atomic
// conditional UPDATE (the SQLite equivalent; local DX).
type SQLJobQueue struct {
	db      *sql.DB
	dialect string
}

// NewSQLJobQueue opens a durable job queue over an existing database handle.
func NewSQLJobQueue(db *sql.DB, dialect string) (*SQLJobQueue, error) {
	q := &SQLJobQueue{db: db, dialect: dialect}
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS jobs (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		payload TEXT NOT NULL DEFAULT '',
		next_run_at INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'queued',
		claimed_by TEXT NOT NULL DEFAULT '',
		lease_until INTEGER NOT NULL DEFAULT 0,
		attempts INTEGER NOT NULL DEFAULT 0,
		last_error TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		return nil, err
	}
	return q, nil
}

func (q *SQLJobQueue) Enqueue(ctx context.Context, j Job) error {
	_, err := q.db.ExecContext(ctx,
		`INSERT INTO jobs (id, type, payload, next_run_at, status) VALUES (?, ?, ?, ?, 'queued')
		 ON CONFLICT(id) DO NOTHING`,
		j.ID, j.Type, j.Payload, j.NextRunAt.UnixMilli())
	return err
}

func (q *SQLJobQueue) Claim(ctx context.Context, nodeID string, lease time.Duration) (Job, bool, error) {
	now := time.Now().UnixMilli()
	leaseUntil := time.Now().Add(lease).UnixMilli()
	if q.dialect == "postgres" {
		return q.claimPostgres(ctx, nodeID, now, leaseUntil)
	}
	return q.claimSQLite(ctx, nodeID, now, leaseUntil)
}

func (q *SQLJobQueue) claimSQLite(ctx context.Context, nodeID string, now, leaseUntil int64) (Job, bool, error) {
	row := q.db.QueryRowContext(ctx, `
		UPDATE jobs SET claimed_by = ?, lease_until = ?, status = 'running', attempts = attempts + 1
		WHERE id = (
			SELECT id FROM jobs
			WHERE status = 'queued' AND next_run_at <= ? AND lease_until <= ?
			ORDER BY next_run_at ASC LIMIT 1
		)
		RETURNING id, type, payload`, nodeID, leaseUntil, now, now)
	var j Job
	if err := row.Scan(&j.ID, &j.Type, &j.Payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Job{}, false, nil
		}
		return Job{}, false, err
	}
	return j, true, nil
}

func (q *SQLJobQueue) claimPostgres(ctx context.Context, nodeID string, now, leaseUntil int64) (Job, bool, error) {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRowContext(ctx, `
		SELECT id, type, payload FROM jobs
		WHERE status = 'queued' AND next_run_at <= $1 AND lease_until <= $1
		ORDER BY next_run_at ASC LIMIT 1
		FOR UPDATE SKIP LOCKED`, now)
	var j Job
	if err := row.Scan(&j.ID, &j.Type, &j.Payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Job{}, false, nil
		}
		return Job{}, false, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE jobs SET claimed_by = $1, lease_until = $2, status = 'running', attempts = attempts + 1 WHERE id = $3`,
		nodeID, leaseUntil, j.ID); err != nil {
		return Job{}, false, err
	}
	return j, true, tx.Commit()
}

func (q *SQLJobQueue) Heartbeat(ctx context.Context, id, nodeID string, lease time.Duration) error {
	res, err := q.db.ExecContext(ctx,
		`UPDATE jobs SET lease_until = ? WHERE id = ? AND claimed_by = ?`,
		time.Now().Add(lease).UnixMilli(), id, nodeID)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

func (q *SQLJobQueue) Complete(ctx context.Context, id, nodeID string) error {
	res, err := q.db.ExecContext(ctx, `DELETE FROM jobs WHERE id = ? AND claimed_by = ?`, id, nodeID)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

func (q *SQLJobQueue) Requeue(ctx context.Context, id, nodeID, errMsg string) error {
	res, err := q.db.ExecContext(ctx,
		`UPDATE jobs SET status = 'queued', claimed_by = '', lease_until = 0, last_error = ? WHERE id = ? AND claimed_by = ?`,
		errMsg, id, nodeID)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

func (q *SQLJobQueue) Reconcile(ctx context.Context, _ string) (int, error) {
	res, err := q.db.ExecContext(ctx,
		`UPDATE jobs SET status = 'queued', claimed_by = '' WHERE status = 'running' AND lease_until <= ?`,
		time.Now().UnixMilli())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func requireAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("scheduler: job not owned by this node")
	}
	return nil
}

var _ JobQueue = (*MemJobQueue)(nil)
var _ JobQueue = (*SQLJobQueue)(nil)
