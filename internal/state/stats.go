package state

import (
	"context"
	"slices"
	"sort"
	"strings"
	"time"
)

// FlowStatsRecord is one flow's stored statistics (spec §2.11.36): its
// current counters and lifetime totals, each a JSON document.
type FlowStatsRecord struct {
	Flow     string
	Current  string
	Lifetime string
}

// StatsSampleRecord is one stored time-series sample (spec §2.11.37): a
// flow's statistics, a JSON document, at a time.
type StatsSampleRecord struct {
	At    time.Time
	Flow  string
	Stats string
}

// SaveFlowStats stores the statistics of the flows in records, replacing
// what those flows had.
func (s *sqlStore) SaveFlowStats(ctx context.Context, records []FlowStatsRecord) error {
	ctx = s.bind(ctx)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for start := 0; start < len(records); start += statsBatch {
		batch := records[start:min(start+statsBatch, len(records))]
		var q strings.Builder
		q.WriteString(`INSERT INTO flow_stats (flow, current_stats, lifetime_stats) VALUES `)
		args := make([]any, 0, 3*len(batch))
		for i, r := range batch {
			if i > 0 {
				q.WriteString(", ")
			}
			q.WriteString("(?, ?, ?)")
			args = append(args, r.Flow, r.Current, r.Lifetime)
		}
		q.WriteString(` ON CONFLICT (flow) DO UPDATE SET current_stats = excluded.current_stats, lifetime_stats = excluded.lifetime_stats`)
		if _, err := tx.ExecContext(ctx, q.String(), args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// FlowStats returns every flow's stored statistics, by flow.
func (s *sqlStore) FlowStats(ctx context.Context) ([]FlowStatsRecord, error) {
	ctx = s.bind(ctx)
	rows, err := s.db.QueryContext(ctx, `SELECT flow, current_stats, lifetime_stats FROM flow_stats ORDER BY flow`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FlowStatsRecord
	for rows.Next() {
		var r FlowStatsRecord
		if err := rows.Scan(&r.Flow, &r.Current, &r.Lifetime); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AppendStatsSamples stores samples.
func (s *sqlStore) AppendStatsSamples(ctx context.Context, samples []StatsSampleRecord) error {
	ctx = s.bind(ctx)
	for start := 0; start < len(samples); start += statsBatch {
		batch := samples[start:min(start+statsBatch, len(samples))]
		var q strings.Builder
		q.WriteString(`INSERT INTO stats_samples (at, flow, stats) VALUES `)
		args := make([]any, 0, 3*len(batch))
		for i, r := range batch {
			if i > 0 {
				q.WriteString(", ")
			}
			q.WriteString("(?, ?, ?)")
			args = append(args, r.At.UnixMilli(), r.Flow, r.Stats)
		}
		if _, err := s.db.ExecContext(ctx, q.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

// StatsSamples returns the newest n samples taken at or after since,
// oldest first.
func (s *sqlStore) StatsSamples(ctx context.Context, since time.Time, n int) ([]StatsSampleRecord, error) {
	ctx = s.bind(ctx)
	rows, err := s.db.QueryContext(ctx, `SELECT at, flow, stats FROM stats_samples WHERE at >= ?
		ORDER BY at DESC, flow /*C*/ DESC LIMIT ?`, since.UnixMilli(), n)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []StatsSampleRecord
	for rows.Next() {
		var r StatsSampleRecord
		var at int64
		if err := rows.Scan(&at, &r.Flow, &r.Stats); err != nil {
			return nil, err
		}
		r.At = time.UnixMilli(at).UTC()
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i] // oldest first
	}
	return out, nil
}

// DeleteStatsSamplesBefore removes the samples taken before t.
func (s *sqlStore) DeleteStatsSamplesBefore(ctx context.Context, t time.Time) error {
	_, err := s.db.ExecContext(s.bind(ctx), `DELETE FROM stats_samples WHERE at < ?`, t.UnixMilli())
	return err
}

// DeleteStatsOf removes a flow's stored statistics and samples.
func (s *sqlStore) DeleteStatsOf(ctx context.Context, flow string) error {
	ctx = s.bind(ctx)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, q := range []string{`DELETE FROM flow_stats WHERE flow = ?`, `DELETE FROM stats_samples WHERE flow = ?`} {
		if _, err := tx.ExecContext(ctx, q, flow); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// statsBatch bounds the rows of one INSERT (3 parameters each).
const statsBatch = 500

// SaveFlowStats stores the statistics of the flows in records.
func (s *MemStore) SaveFlowStats(_ context.Context, records []FlowStatsRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range records {
		if i := slices.IndexFunc(s.flowStats, func(o FlowStatsRecord) bool { return o.Flow == r.Flow }); i >= 0 {
			s.flowStats[i] = r
		} else {
			s.flowStats = append(s.flowStats, r)
		}
	}
	sort.Slice(s.flowStats, func(i, j int) bool { return s.flowStats[i].Flow < s.flowStats[j].Flow })
	return nil
}

// FlowStats returns every flow's statistics, by flow.
func (s *MemStore) FlowStats(context.Context) ([]FlowStatsRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]FlowStatsRecord(nil), s.flowStats...), nil
}

// AppendStatsSamples keeps samples in memory.
func (s *MemStore) AppendStatsSamples(_ context.Context, samples []StatsSampleRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range samples {
		r.At = time.UnixMilli(r.At.UnixMilli()).UTC()
		s.samples = append(s.samples, r)
	}
	sort.SliceStable(s.samples, func(i, j int) bool { return s.samples[i].At.Before(s.samples[j].At) })
	return nil
}

// StatsSamples returns the newest n samples taken at or after since,
// oldest first.
func (s *MemStore) StatsSamples(_ context.Context, since time.Time, n int) ([]StatsSampleRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []StatsSampleRecord
	for _, r := range s.samples {
		if !r.At.Before(since.Truncate(time.Millisecond)) {
			out = append(out, r)
		}
	}
	return out[max(0, len(out)-n):], nil
}

// DeleteStatsSamplesBefore removes the samples taken before t.
func (s *MemStore) DeleteStatsSamplesBefore(_ context.Context, t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t = t.Truncate(time.Millisecond) // as the SQL store compares
	kept := s.samples[:0]
	for _, r := range s.samples {
		if !r.At.Before(t) {
			kept = append(kept, r)
		}
	}
	s.samples = kept
	return nil
}

// DeleteStatsOf removes a flow's statistics and samples.
func (s *MemStore) DeleteStatsOf(_ context.Context, flow string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	keptStats := s.flowStats[:0]
	for _, r := range s.flowStats {
		if r.Flow != flow {
			keptStats = append(keptStats, r)
		}
	}
	s.flowStats = keptStats
	kept := s.samples[:0]
	for _, r := range s.samples {
		if r.Flow != flow {
			kept = append(kept, r)
		}
	}
	s.samples = kept
	return nil
}
