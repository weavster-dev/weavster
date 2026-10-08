package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/weavster-dev/weavster/internal/adapters"
	"github.com/weavster-dev/weavster/internal/flowlife"
	"github.com/weavster-dev/weavster/internal/gateway"
)

// Database source defaults (#107 D-76).
const (
	defaultDBPollInterval = 5 * time.Second
	defaultDBMaxRows      = 100
)

// rowIDMetadata keeps a database source row's id with its message.
const rowIDMetadata = "source.database.id"

// databaseSources polls the query of every started flow with a database
// source and sends each row through the flow like a message sent with the
// API, then marks it with the source's update (#107 D-76). Each flow polls
// in its own goroutine, so a slow flow does not hold up the others; a
// flow's next poll waits for its previous one.
type databaseSources struct {
	flows  flowLister
	ingest messageIngester
	events eventRecorder
	logger *slog.Logger
	dbs    *dbPool
	now    func() time.Time
	listed time.Time
	cached []gateway.Flow
	polls  sync.WaitGroup

	mu      sync.Mutex                 // guards the clock and maps, which the polls update
	clock   pollClock                  // when each flow polls
	running map[string]bool            // flow id -> a poll in progress
	failed  map[string]string          // flow id -> last failure reported
	refused map[string]map[string]bool // flow id -> row ids refused (reported once)
}

func newDatabaseSources(flows flowLister, ingest messageIngester, events eventRecorder, dbs *dbPool, logger *slog.Logger) *databaseSources {
	return &databaseSources{flows: flows, ingest: ingest, events: events, dbs: dbs, logger: logger, now: time.Now,
		clock: newPollClock(), running: map[string]bool{}, failed: map[string]string{}, refused: map[string]map[string]bool{}}
}

// loop polls until ctx is cancelled, then waits for the polls in progress
// (a row being stored is finished first).
func (s *databaseSources) loop(ctx context.Context) {
	ticker := time.NewTicker(sourceTick)
	defer ticker.Stop()
	defer s.polls.Wait()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pass(ctx)
		}
	}
}

// pass starts a poll for every started flow with a database source that
// is due (pollDue) and whose last poll finished; the flow list is read
// at most once per flowRefresh. State of flows that stopped or went away
// is dropped.
func (s *databaseSources) pass(ctx context.Context) {
	now := s.now()
	if now.Sub(s.listed) >= flowRefresh {
		flows, err := s.flows.List(ctx)
		if err != nil {
			if ctx.Err() == nil {
				s.logger.Warn("database sources: listing flows failed", "error", err)
			}
			return
		}
		s.cached, s.listed = flows, now
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	active := map[string]bool{}
	for _, f := range s.cached {
		src := f.Source
		if src == nil || src.Type != "database" || flowlife.Normalize(f.Status) != flowlife.Started {
			continue
		}
		active[f.ID] = true
		if s.running[f.ID] {
			continue
		}
		due, err := s.clock.due(src, f.ID, now, defaultDBPollInterval)
		if err != nil {
			s.fail(f.ID, err.Error())
		}
		if !due {
			continue
		}
		s.running[f.ID] = true
		s.polls.Add(1)
		go func(f gateway.Flow) {
			defer s.polls.Done()
			more, err := s.poll(ctx, f)
			s.mu.Lock()
			defer s.mu.Unlock()
			delete(s.running, f.ID)
			switch {
			case err != nil && ctx.Err() == nil:
				s.fail(f.ID, err.Error())
				if f.Source.Schedule != "" { // a scheduled poll is not skipped for a passing error
					s.clock.retry(f.ID, s.now().Add(defaultDBPollInterval))
				}
			case err == nil:
				delete(s.failed, f.ID)
				if more { // a backlog is read at once, not at the next interval or scheduled time
					s.clock.retry(f.ID, s.now())
				}
			}
		}(f)
	}
	for id := range s.clock.last {
		if !active[id] && !s.running[id] {
			s.clock.forget(id)
			delete(s.failed, id)
			delete(s.refused, id)
		}
	}
}

// poll reads one flow's rows, stores each one, and marks it; a row is
// marked only once its message is stored. more reports that the query
// stopped at a limit after rows were marked, so more may be waiting.
func (s *databaseSources) poll(ctx context.Context, f gateway.Flow) (more bool, err error) {
	src := f.Source
	db, err := s.dbs.open(ctx, src.Driver, src.DSNEnv)
	if err != nil {
		return false, err
	}
	maxRows := defaultDBMaxRows
	if src.MaxRows > 0 {
		maxRows = src.MaxRows
	}
	timeout := time.Duration(src.TimeoutMs) * time.Millisecond
	rows, full, err := adapters.QuerySQL(ctx, db, adapters.SQLQueryOptions{Dialect: src.Driver, Query: src.Query, IDColumn: src.IDColumn, MaxRows: maxRows, Timeout: timeout})
	if err != nil {
		return false, err
	}
	marked := 0
	mark := adapters.SQLUpdate{Dialect: src.Driver, Table: src.Update.Table, Key: src.Update.Key, Set: src.Update.Set, Timeout: timeout}
	for _, row := range rows {
		if ctx.Err() != nil || s.wasRefused(f.ID, row.ID) {
			continue
		}
		if len(row.Body) > gateway.MaxMessageBytes {
			s.refuse(f.ID, row.ID) // as any source refuses a message over the limit
			continue
		}
		res, err := s.ingest.ingest(ctx, f.ID, row.Body, map[string]string{rowIDMetadata: row.ID})
		switch {
		case err == nil, res.ID != "":
			// stored: the flow has the row (a processing failure is the
			// message's, retried or dead-lettered like any other)
		case errors.Is(err, gateway.ErrFlowNotRunning), errors.Is(err, gateway.ErrFlowNotFound):
			return false, nil // stopped meanwhile; the next start reads the row
		case errors.Is(err, gateway.ErrInvalidMessage):
			s.refuse(f.ID, row.ID)
			continue // not stored, so not marked
		case errors.Is(err, gateway.ErrBusy):
			return true, nil // throttled, not failed: the rest at the next poll, which comes at once
		default:
			return false, fmt.Errorf("database: storing a row failed: %w", err)
		}
		if err := mark.Mark(ctx, db, row.ID); err != nil {
			return false, err
		}
		marked++
	}
	// Only when rows were marked: refused rows alone fill the window
	// again, and polling at once would find the same ones.
	return full && marked > 0, nil
}

// fail logs and records a flow's source failure once per reason; the poll
// is retried at the next interval (a scheduled one sooner). Callers hold mu.
func (s *databaseSources) fail(flowID, reason string) {
	if s.failed[flowID] == reason {
		return
	}
	s.failed[flowID] = reason
	s.logger.Warn("database source: poll failed", "flow", flowID, "error", reason)
	s.events.record("source.database.failed", flowID, map[string]string{"reason": reason})
}

// wasRefused reports whether the flow refused the row before.
func (s *databaseSources) wasRefused(flowID, rowID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refused[flowID][rowID]
}

// refuse records once that the flow refused a row; the row is left
// unmarked and not read again while the flow runs.
func (s *databaseSources) refuse(flowID, rowID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refused[flowID] == nil {
		s.refused[flowID] = map[string]bool{}
	}
	s.refused[flowID][rowID] = true
	s.logger.Warn("database source: the flow refused a row", "flow", flowID, "row", rowID)
	s.events.record("source.database.refused", flowID, map[string]string{"row": rowID})
}
