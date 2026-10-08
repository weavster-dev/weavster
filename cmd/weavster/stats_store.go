package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/weavster-dev/weavster/internal/observability"
	"github.com/weavster-dev/weavster/internal/state"
)

// statsRepository keeps flow statistics and their time series (the store:
// PostgreSQL, SQLite, or memory).
type statsRepository interface {
	SaveFlowStats(ctx context.Context, records []state.FlowStatsRecord) error
	FlowStats(ctx context.Context) ([]state.FlowStatsRecord, error)
	AppendStatsSamples(ctx context.Context, samples []state.StatsSampleRecord) error
	StatsSamples(ctx context.Context, since time.Time, n int) ([]state.StatsSampleRecord, error)
	DeleteStatsSamplesBefore(ctx context.Context, t time.Time) error
}

// statsSaveLimit bounds the last save at shutdown.
const statsSaveLimit = 5 * time.Second

// restoreStats loads the stored statistics into stats and the samples
// taken within retention before now (the newest maxStatsPoints) into
// series (#107 D-97).
func restoreStats(ctx context.Context, repo statsRepository, stats *observability.StatsRegistry, series *observability.TimeSeries, retention time.Duration, now time.Time) error {
	records, err := repo.FlowStats(ctx)
	if err != nil {
		return err
	}
	current := make(map[string]observability.FlowStats, len(records))
	lifetime := make(map[string]observability.FlowStats, len(records))
	for _, r := range records {
		var c, l observability.FlowStats
		if err := json.Unmarshal([]byte(r.Current), &c); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(r.Lifetime), &l); err != nil {
			return err
		}
		current[r.Flow], lifetime[r.Flow] = c, l
	}
	samples, err := repo.StatsSamples(ctx, now.Add(-retention), maxStatsPoints)
	if err != nil {
		return err
	}
	points := make([]observability.TimeSeriesPoint, len(samples))
	for i, r := range samples {
		points[i] = observability.TimeSeriesPoint{At: r.At, Flow: r.Flow}
		if err := json.Unmarshal([]byte(r.Stats), &points[i].Stats); err != nil {
			return err
		}
	}
	stats.Load(current, lifetime)
	series.Load(points)
	return nil
}

// save stores the current and lifetime statistics of the flows, the
// samples taken at now (none when sampled is nil), and drops samples older
// than the retention. The caller holds the statistics-writes lock.
func (a statsAdapter) save(ctx context.Context, flows []string, now time.Time, current, lifetime, sampled map[string]observability.FlowStats) error {
	records := make([]state.FlowStatsRecord, 0, len(flows))
	samples := make([]state.StatsSampleRecord, 0, len(sampled))
	for _, f := range flows {
		c, _ := json.Marshal(current[f])
		l, _ := json.Marshal(lifetime[f])
		records = append(records, state.FlowStatsRecord{Flow: f, Current: string(c), Lifetime: string(l)})
		if st, ok := sampled[f]; ok {
			b, _ := json.Marshal(st)
			samples = append(samples, state.StatsSampleRecord{At: now, Flow: f, Stats: string(b)})
		}
	}
	if err := a.repo.SaveFlowStats(ctx, records); err != nil {
		return err
	}
	if err := a.repo.AppendStatsSamples(ctx, samples); err != nil {
		return err
	}
	return a.repo.DeleteStatsSamplesBefore(ctx, now.Add(-a.retention))
}

// saveNow stores the statistics of one flow (every flow when flowID is
// empty) as they are: after a reset, and at shutdown. It waits for the
// definitions lock only as long as ctx allows.
func (a statsAdapter) saveNow(ctx context.Context, flowID string) error {
	if a.repo == nil {
		return nil
	}
	unlock, err := a.flows.definitionsWithin(ctx)
	if err != nil {
		return err
	}
	flows, err := a.flows.List(ctx)
	if err != nil {
		unlock()
		return err
	}
	current, lifetime := a.stats.Snapshots()
	var ids []string
	for _, f := range flows {
		if flowID == "" || f.ID == flowID {
			ids = append(ids, f.ID)
		}
	}
	defer a.flows.statsWrites()() // before unlock: see flowAdapter.statsSaves
	unlock()
	return a.save(ctx, ids, time.Now(), current, lifetime, nil)
}
