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
	file := filepath.Join(t.TempDir(), "src.db")
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
	for _, tt := range []struct {
		name    string
		ingest  *fakeIngest
		stored  func(context.Context, string, string) (bool, error)
		marked  int
		events  []string
		refused bool
		dsnEnv  string
	}{
		{name: "stored and marked", ingest: &fakeIngest{id: "m"}, marked: 2},
		{name: "already stored: only marked", ingest: &fakeIngest{err: errors.New("not called")}, stored: func(context.Context, string, string) (bool, error) { return true, nil }, marked: 2},
		{name: "refused: reported once, not marked", ingest: &fakeIngest{err: gateway.ErrInvalidMessage}, marked: 0, events: []string{"source.database.refused", "source.database.refused"}, refused: true},
		{name: "stopped flow", ingest: &fakeIngest{err: gateway.ErrFlowNotRunning}, marked: 0},
		{name: "ingest failure", ingest: &fakeIngest{err: errors.New("disk full")}, marked: 0, events: []string{"source.database.failed"}},
		{name: "lookup failure", ingest: &fakeIngest{id: "m"}, stored: func(context.Context, string, string) (bool, error) { return false, errors.New("store down") }, events: []string{"source.database.failed"}},
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
			f.Source = &src
			stored := tt.stored
			if stored == nil {
				stored = func(context.Context, string, string) (bool, error) { return false, nil }
			}
			events := &fakeEvents{}
			pool := newDBPool()
			defer pool.close()
			s := newDatabaseSources(&fakeFlowList{flows: []gateway.Flow{f}}, tt.ingest, events, pool, stored, slog.New(slog.NewTextHandler(io.Discard, nil)))
			now := time.Now()
			s.now = func() time.Time { return now }
			for i := 0; i < 2; i++ { // the second poll repeats nothing already reported
				s.pass(context.Background())
				now = now.Add(time.Minute)
			}
			if n := done(); n != tt.marked {
				t.Errorf("%d rows marked, want %d", n, tt.marked)
			}
			if !slices.Equal(events.types, tt.events) {
				t.Errorf("events %v, want %v", events.types, tt.events)
			}
			if tt.refused && len(tt.ingest.files) != 2 {
				t.Errorf("refused rows ingested %d times, want 2 (once each)", len(tt.ingest.files))
			}
		})
	}
	// A flow that stops is forgotten.
	s := newDatabaseSources(&fakeFlowList{flows: []gateway.Flow{{ID: "f", Status: "stopped", Source: flow.Source}}}, &fakeIngest{}, &fakeEvents{}, newDBPool(),
		func(context.Context, string, string) (bool, error) { return false, nil }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.last["f"], s.failed["f"] = time.Now(), "x"
	s.pass(context.Background())
	if len(s.last) != 0 || len(s.failed) != 0 {
		t.Errorf("a stopped flow's state kept: %v %v", s.last, s.failed)
	}
}
