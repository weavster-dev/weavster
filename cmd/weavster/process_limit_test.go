package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestProcessLimit: a free slot is taken at once; with none free a message
// waits up to the limit's wait for one, and is refused as busy when none
// frees up, at once with wait 0, or when the server stops; a cancelled
// caller stops waiting; refusals are logged.
func TestProcessLimit(t *testing.T) {
	for _, tt := range []struct {
		name    string
		waitMs  int
		free    time.Duration // after how long the held slot is freed (0: never)
		stop    bool          // the server stops while it waits
		cancel  bool
		want    error
		atLeast time.Duration
	}{
		{name: "busy after the wait", waitMs: 50, want: gateway.ErrBusy, atLeast: 50 * time.Millisecond},
		{name: "freed during the wait", waitMs: 5000, free: 10 * time.Millisecond},
		{name: "wait 0", want: gateway.ErrBusy},
		{name: "server stopping", waitMs: 5000, stop: true, want: gateway.ErrBusy},
		{name: "caller cancelled", waitMs: 5000, cancel: true, want: context.Canceled},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stopped := make(chan struct{})
			var logs bytes.Buffer
			l := newProcessLimit(serverconfig.Processing{MaxConcurrent: 1, WaitMs: tt.waitMs}, stopped, slog.New(slog.NewTextHandler(&logs, nil)))
			held, err := l.acquire(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if tt.free > 0 {
				go func() { time.Sleep(tt.free); held() }()
			}
			if tt.stop {
				close(stopped)
			}
			ctx, cancel := context.WithCancel(context.Background())
			if tt.cancel {
				cancel()
			}
			defer cancel()
			start := time.Now()
			release, err := l.acquire(ctx)
			if !errors.Is(err, tt.want) || time.Since(start) < tt.atLeast {
				t.Fatalf("acquire = %v after %s, want %v", err, time.Since(start), tt.want)
			}
			if err == nil {
				release()
			}
			if errors.Is(tt.want, gateway.ErrBusy) && !strings.Contains(logs.String(), "processing limit reached") {
				t.Errorf("a refusal was not logged: %s", logs.String())
			}
		})
	}
}
