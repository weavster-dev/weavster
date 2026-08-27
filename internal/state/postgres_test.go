package state

import (
	"context"
	"testing"
	"time"
)

// TestOpenPostgresUnreachable verifies that OpenPostgres surfaces the
// migration connection error instead of hanging or returning a nil error when
// the Postgres backend cannot be reached. It does NOT require a live Postgres:
// it targets an unused loopback port (127.0.0.1:1) with a 1s connect timeout,
// so the pgx driver fails fast with "connection refused" and Migrate returns
// an error that OpenPostgres must propagate.
func TestOpenPostgresUnreachable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store, err := OpenPostgres(ctx, "host=127.0.0.1 port=1 connect_timeout=1")
	if err == nil {
		if store != nil {
			_ = store.Close()
		}
		t.Fatal("expected error when Postgres is unreachable")
	}
}
