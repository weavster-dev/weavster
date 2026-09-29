package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/flowdef"
	"github.com/weavster-dev/weavster/internal/gateway"
)

// TestPollClock: an interval source polls at once, then each interval; a
// scheduled source first polls at the first scheduled time after it was
// seen, then at each scheduled time, in its CRON_TZ.
func TestPollClock(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, tt := range []struct {
		name  string
		src   gateway.FlowSource
		steps map[string]bool // time -> due
	}{
		{"interval", gateway.FlowSource{PollIntervalMs: 60000}, map[string]bool{
			"2026-09-28T10:00:00Z": true, "2026-09-28T10:00:30Z": false, "2026-09-28T10:01:00Z": true,
		}},
		{"default interval", gateway.FlowSource{}, map[string]bool{
			"2026-09-28T10:00:00Z": true, "2026-09-28T10:00:03Z": false, "2026-09-28T10:00:05Z": true,
		}},
		{"every five minutes", gateway.FlowSource{Schedule: "*/5 * * * *"}, map[string]bool{
			"2026-09-28T10:02:00Z": false, // first seen: waits for 10:05
			"2026-09-28T10:04:59Z": false, "2026-09-28T10:05:00Z": true, "2026-09-28T10:06:00Z": false,
			"2026-09-28T10:09:59Z": false, "2026-09-28T10:10:01Z": true,
		}},
		{"daily in Tokyo", gateway.FlowSource{Schedule: "CRON_TZ=Asia/Tokyo 0 9 * * *"}, map[string]bool{
			"2026-09-28T22:00:00Z": false,                               // 07:00 JST
			"2026-09-28T23:59:59Z": false, "2026-09-29T00:00:00Z": true, // 09:00 JST
			"2026-09-29T12:00:00Z": false,
		}},
		{"invalid schedule", gateway.FlowSource{Schedule: "not cron"}, map[string]bool{
			"2026-09-28T10:00:00Z": false, "2026-09-29T10:00:00Z": false,
		}},
		{"a date that never comes", gateway.FlowSource{Schedule: "0 0 30 2 *"}, map[string]bool{
			"2026-09-28T10:00:00Z": false, "2026-09-28T10:00:01Z": false, "2027-02-28T10:00:00Z": false,
		}},
	} {
		clock := newPollClock()
		times := make([]string, 0, len(tt.steps))
		for k := range tt.steps {
			times = append(times, k)
		}
		sort.Strings(times) // RFC 3339 in UTC sorts in time order
		for _, ts := range times {
			if got, _ := clock.due(&tt.src, "f", at(ts), 5*time.Second); got != tt.steps[ts] {
				t.Errorf("%s at %s: due = %v, want %v", tt.name, ts, got, tt.steps[ts])
			}
		}
	}
}

// TestPollClockRetry: a retry time polls before the regular time, once;
// forget drops a flow's times; an invalid schedule is an error.
func TestPollClockRetry(t *testing.T) {
	c := newPollClock()
	src := &gateway.FlowSource{Schedule: "0 6 * * *"}
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	if due, _ := c.due(src, "f", now, time.Second); due {
		t.Fatal("due when first seen")
	}
	c.retry("f", now.Add(time.Minute))
	c.retry("f", now.Add(time.Hour)) // a later retry does not postpone an earlier one
	if due, _ := c.due(src, "f", now.Add(30*time.Second), time.Second); due {
		t.Error("due before the retry time")
	}
	if due, _ := c.due(src, "f", now.Add(time.Minute), time.Second); !due {
		t.Error("not due at the retry time")
	}
	if due, _ := c.due(src, "f", now.Add(2*time.Minute), time.Second); due {
		t.Error("due again after the retry")
	}
	c.forget("f")
	if len(c.last) != 0 || len(c.again) != 0 {
		t.Error("forget kept times")
	}
	if _, err := c.due(&gateway.FlowSource{Schedule: "bad"}, "g", now, time.Second); err == nil {
		t.Error("an invalid schedule: want error")
	}
}

// TestScheduledSourcesCatchUp: at a scheduled time, a file source reads a
// backlog over maxFilesPerPoll at once and looks again at a file still
// being written once it settles; a database source reads rows beyond
// maxRows at once and retries a failed poll soon, not at the next
// scheduled time.
func TestScheduledSourcesCatchUp(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	y, m, d := time.Now().AddDate(0, 0, 1).Date()
	six := time.Date(y, m, d, 6, 0, 0, 0, time.Local) // after the files' real times

	dir := t.TempDir()
	for i := 0; i <= maxFilesPerPoll; i++ {
		settled(t, dir, fmt.Sprintf("f%03d.json", i), "{}")
	}
	fresh := filepath.Join(dir, "zz-fresh.json")
	ing := &fakeIngest{id: "m"}
	fs := newFileSources(&fakeFlowList{flows: []gateway.Flow{{ID: "f", Status: "started", Source: &gateway.FlowSource{Type: "file", Dir: dir, Schedule: "0 6 * * *"}}}},
		ing, &fakeEvents{}, quiet)
	now := six.Add(-time.Minute)
	fs.now = func() time.Time { return now }
	fs.pass(context.Background()) // first seen: waits for 06:00
	now = six
	if err := os.WriteFile(fresh, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(fresh, six, six) // being written at 06:00
	fs.pass(context.Background())
	fs.pass(context.Background()) // the backlog, at once
	if len(ing.files) != maxFilesPerPoll+1 {
		t.Errorf("read %d files at 06:00, want %d", len(ing.files), maxFilesPerPoll+1)
	}
	now = six.Add(fileSettle)
	fs.pass(context.Background())
	if len(ing.files) != maxFilesPerPoll+2 {
		t.Errorf("the file written at 06:00 was not read once settled: %d files", len(ing.files))
	}

	// A file the store could not take is tried again soon.
	storeDown := t.TempDir()
	settled(t, storeDown, "a.json", "{}")
	failing := &fakeIngest{err: errors.New("store down")}
	fd := newFileSources(&fakeFlowList{flows: []gateway.Flow{{ID: "g", Status: "started", Source: &gateway.FlowSource{Type: "file", Dir: storeDown, Schedule: "0 6 * * *"}}}},
		failing, &fakeEvents{}, quiet)
	now = six.Add(-time.Minute)
	fd.now = func() time.Time { return now }
	fd.pass(context.Background())
	now = six
	fd.pass(context.Background())
	if at, ok := fd.clock.again["g"]; !ok || !at.Equal(six.Add(defaultPollInterval)) {
		t.Errorf("a file that could not be stored retries at %v (%v), want %v", at, ok, six.Add(defaultPollInterval))
	}

	file := filepath.Join(t.TempDir(), "src.db") + sqliteShared
	db, err := sql.Open("sqlite", file)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER, done INTEGER DEFAULT 0); INSERT INTO t (id) VALUES (1), (2), (3)`); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WEAVSTER_DB_CATCHUP", file)
	src := &gateway.FlowSource{Type: "database", Driver: "sqlite", DSNEnv: "WEAVSTER_DB_CATCHUP", Query: "SELECT id FROM t WHERE done = 0 ORDER BY id",
		IDColumn: "id", MaxRows: 1, Schedule: "0 6 * * *", Update: &flowdef.SourceUpdate{Table: "t", Key: "id", Set: map[string]string{"done": "1"}}}
	pool := newDBPool()
	defer pool.close()
	ds := newDatabaseSources(&fakeFlowList{flows: []gateway.Flow{{ID: "d", Status: "started", Source: src}}}, &fakeIngest{id: "m"}, &fakeEvents{}, pool, quiet)
	now = six.Add(-time.Minute)
	ds.now = func() time.Time { return now }
	ds.pass(context.Background())
	now = six
	for i := 0; i < 4; i++ {
		ds.pass(context.Background())
		ds.polls.Wait()
	}
	var done int
	_ = db.QueryRow(`SELECT count(*) FROM t WHERE done = 1`).Scan(&done)
	if done != 3 {
		t.Errorf("%d rows read at 06:00 with maxRows 1, want all 3", done)
	}

	unset := *src
	unset.DSNEnv = "WEAVSTER_DB_CATCHUP_UNSET"
	ds = newDatabaseSources(&fakeFlowList{flows: []gateway.Flow{{ID: "d", Status: "started", Source: &unset}}}, &fakeIngest{id: "m"}, &fakeEvents{}, pool, quiet)
	now = six.Add(-time.Minute)
	ds.now = func() time.Time { return now }
	ds.pass(context.Background())
	now = six
	ds.pass(context.Background())
	ds.polls.Wait()
	if at, ok := ds.clock.again["d"]; !ok || !at.Equal(six.Add(defaultDBPollInterval)) {
		t.Errorf("a failed scheduled poll retries at %v (%v), want %v", at, ok, six.Add(defaultDBPollInterval))
	}
}

// TestFileSourceBusy: a file the server was too busy to take stays in the
// directory, is not reported as a failure, and is tried again a moment
// later (also for a scheduled source).
func TestFileSourceBusy(t *testing.T) {
	for _, tt := range []struct {
		name string
		src  gateway.FlowSource
	}{
		{"interval", gateway.FlowSource{Type: "file"}},
		{"scheduled", gateway.FlowSource{Type: "file", Schedule: "0 6 * * *"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := settled(t, dir, "a.json", "{}")
			src := tt.src
			src.Dir = dir
			var logs bytes.Buffer
			fs := newFileSources(&fakeFlowList{flows: []gateway.Flow{{ID: "f", Status: "started", Source: &src}}},
				&fakeIngest{err: gateway.ErrBusy}, &fakeEvents{}, slog.New(slog.NewTextHandler(&logs, nil)))
			y, m, d := time.Now().AddDate(0, 0, 1).Date()
			now := time.Date(y, m, d, 6, 0, 0, 0, time.Local)
			fs.now = func() time.Time { return now }
			if tt.src.Schedule != "" {
				now = now.Add(-time.Minute)
				fs.pass(context.Background()) // first seen: waits for 06:00
				now = now.Add(time.Minute)
			}
			fs.pass(context.Background())
			if _, err := os.Stat(path); err != nil {
				t.Errorf("the file was not kept: %v", err)
			}
			if strings.Contains(logs.String(), "processing failed") || strings.Contains(logs.String(), "level=WARN") {
				t.Errorf("busy was reported as a failure: %s", logs.String())
			}
			if at, ok := fs.clock.again["f"]; !ok || !at.Equal(now.Add(defaultPollInterval)) {
				t.Errorf("tried again at %v (%v), want %v", at, ok, now.Add(defaultPollInterval))
			}
		})
	}
}
