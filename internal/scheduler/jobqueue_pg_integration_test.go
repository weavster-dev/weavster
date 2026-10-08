package scheduler

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver for the integration tests

	"github.com/weavster-dev/weavster/internal/state"
)

// pgQueue is a job queue on a fresh, migrated schema of the PostgreSQL
// database WEAVSTER_TEST_POSTGRES_DSN names (the CI postgres job); without
// it the test is skipped.
func pgQueue(t *testing.T) (*SQLJobQueue, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("WEAVSTER_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("WEAVSTER_TEST_POSTGRES_DSN is not set")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	schema := "t_" + hex.EncodeToString(b)
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
		_ = admin.Close()
	})
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := state.Migrate(context.Background(), db, state.Migrations()); err != nil {
		t.Fatal(err)
	}
	queue, err := NewSQLJobQueue(db)
	if err != nil {
		t.Fatal(err)
	}
	return queue, db
}

// TestPostgresJobLifecycle: on PostgreSQL 16 every queue operation runs: a
// duplicate enqueue is ignored, claims come in due order, only the owner
// can heartbeat, complete, or requeue, and an expired lease is reconciled.
func TestPostgresJobLifecycle(t *testing.T) {
	q, _ := pgQueue(t)
	ctx := context.Background()
	past := time.Now().Add(-time.Minute)
	for _, j := range []Job{{ID: "b", Type: "poll", Payload: "{}", NextRunAt: past}, {ID: "a", Type: "poll", Payload: "{}", NextRunAt: past},
		{ID: "z-earliest", Type: "poll", NextRunAt: past.Add(-time.Minute)}, {ID: "later", Type: "poll", NextRunAt: time.Now().Add(time.Hour)}} {
		if err := q.Enqueue(ctx, j); err != nil {
			t.Fatalf("enqueue %s: %v", j.ID, err)
		}
	}
	if err := q.Enqueue(ctx, Job{ID: "a", Type: "other", NextRunAt: past}); err != nil {
		t.Fatalf("duplicate enqueue: %v", err)
	}

	earliest, ok, err := q.Claim(ctx, "node-1", time.Minute)
	if err != nil || !ok || earliest.ID != "z-earliest" {
		t.Fatalf("first claim = %+v %v %v, want z-earliest (due first, whatever its id)", earliest, ok, err)
	}
	if err := q.Complete(ctx, "z-earliest", "node-1"); err != nil {
		t.Fatal(err)
	}
	first, ok, err := q.Claim(ctx, "node-1", time.Minute)
	if err != nil || !ok || first.ID != "a" || first.Type != "poll" {
		t.Fatalf("second claim = %+v %v %v, want a (same due time: by id; the duplicate ignored)", first, ok, err)
	}
	if err := q.Heartbeat(ctx, "a", "node-2", time.Minute); err == nil {
		t.Error("another node extended the lease")
	}
	if err := q.Heartbeat(ctx, "a", "node-1", time.Minute); err != nil {
		t.Errorf("heartbeat: %v", err)
	}
	if err := q.Requeue(ctx, "a", "node-1", "boom"); err != nil {
		t.Errorf("requeue: %v", err)
	}
	again, ok, err := q.Claim(ctx, "node-1", time.Minute)
	if err != nil || !ok || again.ID != "a" {
		t.Fatalf("claim after requeue = %+v %v %v", again, ok, err)
	}
	if err := q.Complete(ctx, "a", "node-2"); err == nil {
		t.Error("another node completed the job")
	}
	if err := q.Complete(ctx, "a", "node-1"); err != nil {
		t.Errorf("complete: %v", err)
	}

	// A claim whose lease has run out is put back by Reconcile.
	second, ok, err := q.Claim(ctx, "node-1", -time.Second)
	if err != nil || !ok || second.ID != "b" {
		t.Fatalf("second claim = %+v %v %v", second, ok, err)
	}
	if n, err := q.Reconcile(ctx, "node-2"); err != nil || n != 1 {
		t.Errorf("reconcile = %d %v, want 1", n, err)
	}
	if j, ok, err := q.Claim(ctx, "node-2", time.Minute); err != nil || !ok || j.ID != "b" {
		t.Errorf("claim after reconcile = %+v %v %v", j, ok, err)
	}
	if j, ok, err := q.Claim(ctx, "node-2", time.Minute); err != nil || ok {
		t.Errorf("claim with nothing due = %+v %v %v", j, ok, err)
	}
}

// TestPostgresClaimSkipLocked: a job row another transaction has locked is
// skipped at once (FOR UPDATE SKIP LOCKED), not waited for.
func TestPostgresClaimSkipLocked(t *testing.T) {
	q, db := pgQueue(t)
	ctx := context.Background()
	past := time.Now().Add(-time.Minute)
	for _, id := range []string{"a", "b"} {
		if err := q.Enqueue(ctx, Job{ID: id, Type: "poll", NextRunAt: past}); err != nil {
			t.Fatal(err)
		}
	}
	other, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Rollback() }()
	if _, err := other.ExecContext(ctx, `SELECT id FROM jobs WHERE id = 'a' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}

	quick, cancel := context.WithTimeout(ctx, 3*time.Second) // a blocked claim would run into this
	defer cancel()
	j, ok, err := q.Claim(quick, "node-1", time.Minute)
	if err != nil || !ok || j.ID != "b" {
		t.Fatalf("claim with a locked = %+v %v %v, want b at once", j, ok, err)
	}
	if j, ok, err := q.Claim(quick, "node-1", time.Minute); err != nil || ok {
		t.Errorf("claim with only a locked = %+v %v %v, want none", j, ok, err)
	}
}

// TestPostgresConcurrentClaims: claimers racing on one queue take every
// job exactly once.
func TestPostgresConcurrentClaims(t *testing.T) {
	q, _ := pgQueue(t)
	ctx := context.Background()
	const jobs, workers = 60, 8
	for i := range jobs {
		if err := q.Enqueue(ctx, Job{ID: fmt.Sprintf("j%02d", i), Type: "poll", NextRunAt: time.Now().Add(-time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	claimed := map[string]int{}
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			node := fmt.Sprintf("node-%d", w)
			for {
				j, ok, err := q.Claim(ctx, node, time.Minute)
				if err != nil {
					t.Error(err)
					return
				}
				if !ok {
					return
				}
				mu.Lock()
				claimed[j.ID]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(claimed) != jobs {
		t.Errorf("%d jobs claimed, want %d", len(claimed), jobs)
	}
	for id, n := range claimed {
		if n != 1 {
			t.Errorf("%s claimed %d times", id, n)
		}
	}
}

// TestPostgresClaimByteOrder: jobs due at the same time are claimed in id
// byte order ("B" before "a"), as on SQLite, whatever the database's locale.
func TestPostgresClaimByteOrder(t *testing.T) {
	q, _ := pgQueue(t)
	ctx := context.Background()
	due := time.Now().Add(-time.Minute)
	for _, id := range []string{"a-job", "B-job"} {
		if err := q.Enqueue(ctx, Job{ID: id, Type: "poll", NextRunAt: due}); err != nil {
			t.Fatal(err)
		}
	}
	if j, ok, err := q.Claim(ctx, "node-1", time.Minute); err != nil || !ok || j.ID != "B-job" {
		t.Errorf("claim = %+v %v %v, want B-job", j, ok, err)
	}
}
