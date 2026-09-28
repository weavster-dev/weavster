package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestProcessLimit: a free slot is taken at once; with none free, a
// message waits for one up to the limit's wait, then is refused as busy;
// a cancelled caller stops waiting.
func TestProcessLimit(t *testing.T) {
	l := newProcessLimit(serverconfig.Processing{MaxConcurrent: 1, WaitMs: 50})
	release, err := l.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := l.acquire(context.Background()); !errors.Is(err, gateway.ErrBusy) || time.Since(start) < 50*time.Millisecond {
		t.Errorf("full: %v after %s, want ErrBusy after the wait", err, time.Since(start))
	}
	go func() { time.Sleep(10 * time.Millisecond); release() }()
	again, err := l.acquire(context.Background())
	if err != nil {
		t.Fatalf("a slot freed during the wait: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
	again()
	now := newProcessLimit(serverconfig.Processing{MaxConcurrent: 1})
	r, _ := now.acquire(context.Background())
	if _, err := now.acquire(context.Background()); !errors.Is(err, gateway.ErrBusy) {
		t.Errorf("waitMs 0: %v, want ErrBusy at once", err)
	}
	r()
}
