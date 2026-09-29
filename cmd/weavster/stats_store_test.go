package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/observability"
	"github.com/weavster-dev/weavster/internal/state"
)

// failingStats fails the one statsRepository operation named by op.
type failingStats struct {
	*state.MemStore
	op string
}

var errStatsStore = errors.New("stats store down")

func (f failingStats) fail(op string) error {
	if f.op == op {
		return errStatsStore
	}
	return nil
}

func (f failingStats) SaveFlowStats(ctx context.Context, r []state.FlowStatsRecord) error {
	if err := f.fail("save"); err != nil {
		return err
	}
	return f.MemStore.SaveFlowStats(ctx, r)
}

func (f failingStats) FlowStats(ctx context.Context) ([]state.FlowStatsRecord, error) {
	if err := f.fail("stats"); err != nil {
		return nil, err
	}
	return f.MemStore.FlowStats(ctx)
}

func (f failingStats) AppendStatsSamples(ctx context.Context, s []state.StatsSampleRecord) error {
	if err := f.fail("append"); err != nil {
		return err
	}
	return f.MemStore.AppendStatsSamples(ctx, s)
}

func (f failingStats) StatsSamples(ctx context.Context, since time.Time, n int) ([]state.StatsSampleRecord, error) {
	if err := f.fail("samples"); err != nil {
		return nil, err
	}
	return f.MemStore.StatsSamples(ctx, since, n)
}

// TestStatsStore: a sample stores the statistics and the sample, a reset
// is stored at once, deleting a flow removes its stored statistics, and
// stored statistics load back; store failures are reported.
func TestStatsStore(t *testing.T) {
	ctx := context.Background()
	mem := state.NewMemStore()
	stats := observability.NewStatsRegistry()
	var logs bytes.Buffer
	flows := flowAdapter{store: mem, stats: stats, statsSaves: &sync.Mutex{}, defs: &sync.Mutex{}}
	a := statsAdapter{flows: flows, stats: stats, series: observability.NewTimeSeries(time.Hour, 100), repo: mem, retention: time.Hour,
		logger: slog.New(slog.NewTextHandler(&logs, nil))}
	if _, err := flows.Create(ctx, gateway.Flow{ID: "adt"}); err != nil {
		t.Fatal(err)
	}
	stats.Inc("adt", observability.Sent)
	now := time.Now()
	if err := a.sample(ctx, now); err != nil {
		t.Fatal(err)
	}
	if recs, _ := mem.FlowStats(ctx); len(recs) != 1 || recs[0].Lifetime != `{"received":0,"filtered":0,"transformed":0,"sent":1,"errored":0,"queued":0}` {
		t.Errorf("stored stats = %+v", recs)
	}
	if samples, _ := mem.StatsSamples(ctx, time.Time{}, 10); len(samples) != 1 || samples[0].Flow != "adt" {
		t.Errorf("stored samples = %+v", samples)
	}

	// Stored statistics load back into a new registry and series.
	stats2, series2 := observability.NewStatsRegistry(), observability.NewTimeSeries(time.Hour, 100)
	if err := restoreStats(ctx, mem, stats2, series2, time.Hour, now); err != nil {
		t.Fatal(err)
	}
	if got := stats2.Snapshot("adt", true); got.Sent != 1 {
		t.Errorf("restored = %+v", got)
	}
	if got := series2.Series(func(string) bool { return true }, time.Time{}, time.Time{}, 0); len(got) != 1 {
		t.Errorf("restored series = %+v", got)
	}

	if err := a.ResetStats(ctx, "adt", true); err != nil {
		t.Fatal(err)
	}
	if recs, _ := mem.FlowStats(ctx); len(recs) != 1 || recs[0].Lifetime != `{"received":0,"filtered":0,"transformed":0,"sent":0,"errored":0,"queued":0}` {
		t.Errorf("stored after a reset = %+v", recs)
	}

	// A reset the store cannot take is still done, and logged.
	stats.Inc("adt", observability.Sent)
	a.repo = failingStats{mem, "save"}
	if err := a.ResetStats(ctx, "adt", false); err != nil || stats.Snapshot("adt", false).Sent != 0 {
		t.Errorf("reset with a failing store = %v", err)
	}
	if !strings.Contains(logs.String(), "statistics reset not stored yet") {
		t.Errorf("log = %q", logs.String())
	}

	for _, op := range []string{"save", "append"} {
		a.repo = failingStats{mem, op}
		if err := a.sample(ctx, time.Now()); !errors.Is(err, errStatsStore) {
			t.Errorf("sample with %s failing = %v", op, err)
		}
	}

	// At shutdown, the save waits for the definitions only as long as allowed.
	a.repo = mem
	unlock := a.flows.definitions()
	short, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if err := a.saveNow(short, ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("save while the definitions are held = %v", err)
	}
	unlock()
	a.repo = nil
	if err := a.saveNow(ctx, ""); err != nil {
		t.Errorf("saving without a store = %v", err)
	}

	// Deleting the flow removes its stored statistics with it.
	if err := a.flows.Delete(ctx, "adt"); err != nil {
		t.Fatal(err)
	}
	recs, _ := mem.FlowStats(ctx)
	samples, _ := mem.StatsSamples(ctx, time.Time{}, 10)
	if len(recs) != 0 || len(samples) != 0 {
		t.Errorf("after the delete: %+v %+v", recs, samples)
	}
}

// TestRestoreStatsErrors: a store that fails or holds unreadable
// statistics stops the restore.
func TestRestoreStatsErrors(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name   string
		repo   func() statsRepository
		stored []state.FlowStatsRecord
		sample string
	}{
		{name: "stats fail", repo: func() statsRepository { return failingStats{state.NewMemStore(), "stats"} }},
		{name: "samples fail", repo: func() statsRepository { return failingStats{state.NewMemStore(), "samples"} }},
		{name: "bad current", stored: []state.FlowStatsRecord{{Flow: "a", Current: "x", Lifetime: "{}"}}},
		{name: "bad lifetime", stored: []state.FlowStatsRecord{{Flow: "a", Current: "{}", Lifetime: "x"}}},
		{name: "bad sample", sample: "x"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var repo statsRepository = state.NewMemStore()
			if tt.repo != nil {
				repo = tt.repo()
			}
			_ = repo.SaveFlowStats(ctx, tt.stored)
			if tt.sample != "" {
				_ = repo.AppendStatsSamples(ctx, []state.StatsSampleRecord{{At: time.Now(), Flow: "a", Stats: tt.sample}})
			}
			err := restoreStats(ctx, repo, observability.NewStatsRegistry(), observability.NewTimeSeries(time.Hour, 10), time.Hour, time.Now())
			if err == nil {
				t.Error("restored")
			}
		})
	}
}
