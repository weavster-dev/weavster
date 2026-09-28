package main

import (
	"context"
	"time"

	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// processLimit bounds the messages received and processed at once (#107
// D-79): a slot per message, and a bounded wait for one when all are busy.
type processLimit struct {
	slots chan struct{}
	wait  time.Duration
}

func newProcessLimit(cfg serverconfig.Processing) *processLimit {
	return &processLimit{slots: make(chan struct{}, cfg.MaxConcurrent), wait: time.Duration(cfg.WaitMs) * time.Millisecond}
}

// acquire takes a slot, waiting up to the limit's wait; it returns
// gateway.ErrBusy when none frees up in time, or ctx's error. The returned
// func gives the slot back.
func (l *processLimit) acquire(ctx context.Context) (func(), error) {
	release := func() { <-l.slots }
	select {
	case l.slots <- struct{}{}:
		return release, nil
	default:
	}
	timer := time.NewTimer(l.wait)
	defer timer.Stop()
	select {
	case l.slots <- struct{}{}:
		return release, nil
	case <-timer.C:
		return nil, gateway.ErrBusy
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
