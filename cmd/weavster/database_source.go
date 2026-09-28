package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
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

	mu      sync.Mutex                 // guards the maps, which the polls update
	last    map[string]time.Time       // flow id -> last poll started
	running map[string]bool            // flow id -> a poll in progress
	failed  map[string]string          // flow id -> last failure reported
	refused map[string]map[string]bool // flow id -> row ids refused (reported once)
}

func newDatabaseSources(flows flowLister, ingest messageIngester, events eventRecorder, dbs *dbPool, logger *slog.Logger) *databaseSources {
	return &databaseSources{flows: flows, ingest: ingest, events: events, dbs: dbs, logger: logger, now: time.Now,
		last: map[string]time.Time{}, running: map[string]bool{}, failed: map[string]string{}, refused: map[string]map[string]bool{}}
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

// pass starts a poll for every started flow with a database source whose
// interval has passed and whose last poll finished; the flow list is read
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
		interval := defaultDBPollInterval
		if src.PollIntervalMs > 0 {
			interval = time.Duration(src.PollIntervalMs) * time.Millisecond
		}
		if s.running[f.ID] || now.Sub(s.last[f.ID]) < interval {
			continue
		}
		s.last[f.ID], s.running[f.ID] = now, true
		s.polls.Add(1)
		go func(f gateway.Flow) {
			defer s.polls.Done()
			err := s.poll(ctx, f)
			s.mu.Lock()
			defer s.mu.Unlock()
			delete(s.running, f.ID)
			switch {
			case err != nil && ctx.Err() == nil:
				s.fail(f.ID, err.Error())
			case err == nil:
				delete(s.failed, f.ID)
			}
		}(f)
	}
	for id := range s.last {
		if !active[id] && !s.running[id] {
			delete(s.last, id)
			delete(s.failed, id)
			delete(s.refused, id)
		}
	}
}

// poll reads one flow's rows, stores each one, and marks it; a row is
// marked only once its message is stored.
func (s *databaseSources) poll(ctx context.Context, f gateway.Flow) error {
	src := f.Source
	dsn := os.Getenv(src.DSNEnv)
	if dsn == "" {
		return fmt.Errorf("database: environment variable %s is not set", src.DSNEnv)
	}
	db, err := s.dbs.get(src.Driver, src.DSNEnv, dsn)
	if err != nil {
		return err
	}
	maxRows := defaultDBMaxRows
	if src.MaxRows > 0 {
		maxRows = src.MaxRows
	}
	timeout := time.Duration(src.TimeoutMs) * time.Millisecond
	rows, err := adapters.QuerySQL(ctx, db, adapters.SQLQueryOptions{Dialect: src.Driver, Query: src.Query, IDColumn: src.IDColumn, MaxRows: maxRows, Timeout: timeout})
	if err != nil {
		return err
	}
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
			return nil // stopped meanwhile; the next start reads the row
		case errors.Is(err, gateway.ErrInvalidMessage):
			s.refuse(f.ID, row.ID)
			continue // not stored, so not marked
		default:
			return fmt.Errorf("database: storing a row failed: %w", err)
		}
		if err := mark.Mark(ctx, db, row.ID); err != nil {
			return err
		}
	}
	return nil
}

// fail logs and records a flow's source failure once per reason; the poll
// is retried at the next interval. Callers hold mu.
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
