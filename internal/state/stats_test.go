package state

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// statsStore is a backend that keeps statistics.
type statsStore interface {
	SaveFlowStats(ctx context.Context, records []FlowStatsRecord) error
	FlowStats(ctx context.Context) ([]FlowStatsRecord, error)
	AppendStatsSamples(ctx context.Context, samples []StatsSampleRecord) error
	StatsSamples(ctx context.Context, since time.Time, n int) ([]StatsSampleRecord, error)
	DeleteStatsSamplesBefore(ctx context.Context, t time.Time) error
	DeleteStatsOf(ctx context.Context, flow string) error
}

// TestStats: flow statistics are stored per flow, samples are read
// back newest-n oldest first from a time on, and old samples or a flow's
// statistics can be deleted, on every backend.
func TestStats(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	for name, s := range testBackends(t) {
		ss := s.(statsStore)
		if got, err := ss.FlowStats(ctx); err != nil || len(got) != 0 {
			t.Fatalf("%s: empty store = %v %v", name, got, err)
		}
		if err := ss.SaveFlowStats(ctx, []FlowStatsRecord{{Flow: "lab", Current: `{"sent":1}`, Lifetime: `{"sent":9}`}, {Flow: "adt", Current: "{}", Lifetime: "{}"}}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := ss.SaveFlowStats(ctx, []FlowStatsRecord{{Flow: "lab", Current: `{"sent":2}`, Lifetime: `{"sent":10}`}, {Flow: "rad", Current: "{}", Lifetime: "{}"}}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got, err := ss.FlowStats(ctx); err != nil || len(got) != 3 || got[0].Flow != "adt" || got[1].Flow != "lab" || got[1].Lifetime != `{"sent":10}` || got[2].Flow != "rad" {
			t.Errorf("%s: updated stats = %+v %v", name, got, err)
		}

		var samples []StatsSampleRecord
		for i := range 4 {
			for _, f := range []string{"adt", "lab"} {
				samples = append(samples, StatsSampleRecord{At: base.Add(time.Duration(i) * time.Minute), Flow: f, Stats: fmt.Sprintf(`{"sent":%d}`, i)})
			}
		}
		if err := ss.AppendStatsSamples(ctx, samples); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, err := ss.StatsSamples(ctx, base.Add(time.Minute), 3)
		if err != nil || len(got) != 3 || !got[0].At.Equal(base.Add(2*time.Minute)) || got[0].Flow != "lab" ||
			got[2].Flow != "lab" || got[2].Stats != `{"sent":3}` {
			t.Errorf("%s: newest 3 = %+v %v", name, got, err)
		}
		if got, _ := ss.StatsSamples(ctx, base.Add(2*time.Minute), 100); len(got) != 4 {
			t.Errorf("%s: since the third minute = %d samples", name, len(got))
		}
		if err := ss.DeleteStatsSamplesBefore(ctx, base.Add(2*time.Minute+time.Microsecond)); err != nil { // milliseconds count
			t.Fatalf("%s: %v", name, err)
		}
		if kept, _ := ss.StatsSamples(ctx, time.Time{}, 100); len(kept) != 4 || !kept[0].At.Equal(base.Add(2*time.Minute)) {
			t.Errorf("%s: after the retention cut = %+v", name, kept)
		}
		if err := ss.DeleteStatsOf(ctx, "lab"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := ss.DeleteStatsOf(ctx, "adt"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		left, _ := ss.StatsSamples(ctx, time.Time{}, 100)
		stats, _ := ss.FlowStats(ctx)
		if len(left) != 0 || len(stats) != 1 || stats[0].Flow != "rad" {
			t.Errorf("%s: left %+v and %+v", name, left, stats)
		}
	}
}

// TestStatsBatches: more rows than one INSERT holds are all stored.
func TestStatsBatches(t *testing.T) {
	ctx := context.Background()
	for name, s := range testBackends(t) {
		ss := s.(statsStore)
		n := statsBatch + 3
		records := make([]FlowStatsRecord, n)
		samples := make([]StatsSampleRecord, n)
		for i := range n {
			records[i] = FlowStatsRecord{Flow: fmt.Sprintf("f%04d", i), Current: "{}", Lifetime: "{}"}
			samples[i] = StatsSampleRecord{At: time.UnixMilli(int64(i)), Flow: "f", Stats: "{}"}
		}
		if err := ss.SaveFlowStats(ctx, records); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := ss.AppendStatsSamples(ctx, samples); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, _ := ss.FlowStats(ctx)
		all, _ := ss.StatsSamples(ctx, time.Time{}, 2*n)
		if len(got) != n || len(all) != n {
			t.Errorf("%s: %d stats, %d samples; want %d", name, len(got), len(all), n)
		}
	}
}
