package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
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

// rowIDMetadata is the metadata that keeps a database source row's id with
// its message, so the row is not stored again.
const rowIDMetadata = "source.database.id"

// databaseSources polls the query of every started flow with a database
// source and sends each row through the flow like a message sent with the
// API (#107 D-76). It runs in one goroutine, so its maps need no lock.
type databaseSources struct {
	flows  flowLister
	ingest messageIngester
	events eventRecorder
	logger *slog.Logger
	dbs    *dbPool
	// stored reports whether flowID already stored the row with rowID.
	stored  func(ctx context.Context, flowID, rowID string) (bool, error)
	now     func() time.Time
	listed  time.Time
	cached  []gateway.Flow
	last    map[string]time.Time       // flow id -> last poll
	failed  map[string]string          // flow id -> last failure reported
	refused map[string]map[string]bool // flow id -> row ids refused (reported once)
}

func newDatabaseSources(flows flowLister, ingest messageIngester, events eventRecorder, dbs *dbPool,
	stored func(ctx context.Context, flowID, rowID string) (bool, error), logger *slog.Logger) *databaseSources {
	return &databaseSources{flows: flows, ingest: ingest, events: events, dbs: dbs, stored: stored, logger: logger, now: time.Now,
		last: map[string]time.Time{}, failed: map[string]string{}, refused: map[string]map[string]bool{}}
}

// loop polls until ctx is cancelled; a row in progress is finished first.
func (s *databaseSources) loop(ctx context.Context) {
	ticker := time.NewTicker(sourceTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pass(ctx)
		}
	}
}

// pass polls every started flow with a database source whose interval has
// passed; the flow list is read at most once per flowRefresh.
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
	for _, f := range s.cached {
		src := f.Source
		if src == nil || src.Type != "database" || flowlife.Normalize(f.Status) != flowlife.Started {
			delete(s.last, f.ID)
			delete(s.failed, f.ID)
			delete(s.refused, f.ID)
			continue
		}
		interval := defaultDBPollInterval
		if src.PollIntervalMs > 0 {
			interval = time.Duration(src.PollIntervalMs) * time.Millisecond
		}
		if now.Sub(s.last[f.ID]) < interval {
			continue
		}
		s.last[f.ID] = now
		if err := s.poll(ctx, f); err != nil && ctx.Err() == nil {
			s.fail(f.ID, err.Error())
		} else if err == nil {
			delete(s.failed, f.ID)
		}
		if ctx.Err() != nil {
			return
		}
	}
}

// poll reads one flow's rows, stores the new ones, and marks each stored
// row with the source's update.
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
	var mark *adapters.SQLUpdate
	if u := src.Update; u != nil {
		mark = &adapters.SQLUpdate{Dialect: src.Driver, Table: u.Table, Key: u.Key, Set: u.Set, Timeout: timeout}
	}
	for _, row := range rows {
		if s.refused[f.ID][row.ID] {
			continue
		}
		seen, err := s.stored(ctx, f.ID, row.ID)
		if err != nil {
			return fmt.Errorf("database: looking up stored rows failed: %w", err)
		}
		if !seen {
			body, _ := json.Marshal(row.Values) // column values are JSON values
			res, err := s.ingest.ingest(ctx, f.ID, body, map[string]string{rowIDMetadata: row.ID})
			switch {
			case err == nil, res.ID != "":
				// stored: the flow has the row, and reports its own failures
			case errors.Is(err, gateway.ErrFlowNotRunning), errors.Is(err, gateway.ErrFlowNotFound):
				return nil // stopped meanwhile; the next start reads the row
			case errors.Is(err, gateway.ErrInvalidMessage):
				s.refuse(f.ID, row.ID)
				continue // not marked: it was not stored
			default:
				return fmt.Errorf("database: storing a row failed: %w", err)
			}
		}
		if mark != nil {
			if err := mark.Mark(ctx, db, row.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// fail logs and records a flow's source failure once per reason; the poll
// is retried at the next interval.
func (s *databaseSources) fail(flowID, reason string) {
	if s.failed[flowID] == reason {
		return
	}
	s.failed[flowID] = reason
	s.logger.Warn("database source: poll failed", "flow", flowID, "error", reason)
	s.events.record("source.database.failed", flowID, map[string]string{"reason": reason})
}

// refuse records once that the flow refused a row; the row is left
// unmarked and not read again while the flow runs.
func (s *databaseSources) refuse(flowID, rowID string) {
	if s.refused[flowID] == nil {
		s.refused[flowID] = map[string]bool{}
	}
	s.refused[flowID][rowID] = true
	s.logger.Warn("database source: the flow refused a row", "flow", flowID, "row", rowID)
	s.events.record("source.database.refused", flowID, map[string]string{"row": rowID})
}
