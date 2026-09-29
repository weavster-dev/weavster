package main

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/flowdef"
	"github.com/weavster-dev/weavster/internal/gateway"
)

// TestDatabaseSourcePoll: what a poll does with each ingest outcome and
// failure: stored rows are marked, refused rows are reported once and left
// unmarked, a stopped flow ends the poll, and failures are recorded once
// per reason.
func TestDatabaseSourcePoll(t *testing.T) {
	file := filepath.Join(t.TempDir(), "src.db") + sqliteShared
	db, err := sql.Open("sqlite", file)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER, done INTEGER DEFAULT 0); INSERT INTO t (id) VALUES (1), (2)`); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WEAVSTER_DB_POLL", file)
	flow := gateway.Flow{ID: "f", Status: "started", Source: &flowdef.Source{Type: "database", Driver: "sqlite", DSNEnv: "WEAVSTER_DB_POLL",
		Query: "SELECT id FROM t WHERE done = 0 ORDER BY id", IDColumn: "id", Update: &flowdef.SourceUpdate{Table: "t", Key: "id", Set: map[string]string{"done": "1"}}}}
	done := func() (n int) {
		_ = db.QueryRow(`SELECT count(*) FROM t WHERE done = 1`).Scan(&n)
		return n
	}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tt := range []struct {
		name    string
		ingest  *fakeIngest
		update  *flowdef.SourceUpdate
		marked  int
		events  []string
		ingests int
		dsnEnv  string
		query   string
	}{
		{name: "stored and marked", ingest: &fakeIngest{id: "m"}, marked: 2, ingests: 2},
		{name: "stored, processing failed: marked", ingest: &fakeIngest{id: "m", err: errors.New("transform failed")}, marked: 2, ingests: 2},
		{name: "refused: reported once, not marked", ingest: &fakeIngest{err: gateway.ErrInvalidMessage}, events: []string{"source.database.refused", "source.database.refused"}, ingests: 2},
		{name: "stopped flow", ingest: &fakeIngest{err: gateway.ErrFlowNotRunning}, ingests: 2},
		{name: "busy: throttled, not failed, not marked", ingest: &fakeIngest{err: gateway.ErrBusy}, ingests: 2},
		{name: "ingest failure", ingest: &fakeIngest{err: errors.New("disk full")}, events: []string{"source.database.failed"}, ingests: 2},
		{name: "update failure", ingest: &fakeIngest{id: "m"}, update: &flowdef.SourceUpdate{Table: "nope", Key: "id", Set: map[string]string{"done": "1"}}, events: []string{"source.database.failed"}, ingests: 2},
		{name: "over the size limit: refused", ingest: &fakeIngest{id: "m"}, query: "SELECT id, zeroblob(10485761) AS big FROM t WHERE done = 0", events: []string{"source.database.refused", "source.database.refused"}},
		{name: "unset variable", ingest: &fakeIngest{id: "m"}, dsnEnv: "WEAVSTER_DB_UNSET_POLL", events: []string{"source.database.failed"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := db.Exec(`UPDATE t SET done = 0`); err != nil {
				t.Fatal(err)
			}
			f := flow
			src := *f.Source
			if tt.dsnEnv != "" {
				src.DSNEnv = tt.dsnEnv
			}
			if tt.update != nil {
				src.Update = tt.update
			}
			if tt.query != "" {
				src.Query = tt.query
			}
			f.Source = &src
			events := &fakeEvents{}
			pool := newDBPool()
			defer pool.close()
			s := newDatabaseSources(&fakeFlowList{flows: []gateway.Flow{f}}, tt.ingest, events, pool, quiet)
			now := time.Now()
			s.now = func() time.Time { return now }
			for i := 0; i < 2; i++ { // the second poll reports nothing already reported
				s.pass(context.Background())
				s.polls.Wait()
				now = now.Add(time.Minute)
			}
			if n := done(); n != tt.marked {
				t.Errorf("%d rows marked, want %d", n, tt.marked)
			}
			if !slices.Equal(events.types, tt.events) {
				t.Errorf("events %v, want %v", events.types, tt.events)
			}
			if len(tt.ingest.files) < tt.ingests {
				t.Errorf("%d ingests, want at least %d", len(tt.ingest.files), tt.ingests)
			}
		})
	}

	// A poll in progress is not started again; a flow that stops or goes
	// away is forgotten.
	list := &fakeFlowList{flows: []gateway.Flow{flow}}
	s := newDatabaseSources(list, &fakeIngest{id: "m"}, &fakeEvents{}, newDBPool(), quiet)
	s.running["f"] = true
	s.pass(context.Background())
	if !s.clock.last["f"].IsZero() {
		t.Error("a second poll started while one was running")
	}
	delete(s.running, "f")
	s.clock.last["f"], s.failed["f"], s.refused["f"] = time.Now(), "x", map[string]bool{"1": true}
	list.flows = nil
	s.listed = time.Time{}
	s.pass(context.Background())
	if len(s.clock.last) != 0 || len(s.failed) != 0 || len(s.refused) != 0 {
		t.Errorf("a removed flow's state kept: %v %v %v", s.clock.last, s.failed, s.refused)
	}
}
