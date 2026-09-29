package observability

import (
	"encoding/json"
	"os"
	"slices"
	"sort"
	"sync"
	"time"
)

// CounterKind enumerates the per-flow statistic counters (spec §2.11.36).
type CounterKind int

const (
	// Received counts messages acquired from a source.
	Received CounterKind = iota
	// Filtered counts messages rejected by a filter.
	Filtered
	// Transformed counts messages passed through a transform.
	Transformed
	// Sent counts successfully delivered messages.
	Sent
	// Errored counts failed messages.
	Errored
	// Queued counts messages retained for retry.
	Queued
)

// ConnectorStats counts per-connector (source/destination) traffic.
type ConnectorStats struct {
	Received int64 `json:"received"`
	Sent     int64 `json:"sent"`
	Errored  int64 `json:"errored"`
	Queued   int64 `json:"queued"`
}

// FlowStats holds per-flow counters (spec §2.11.36).
type FlowStats struct {
	Received    int64                     `json:"received"`
	Filtered    int64                     `json:"filtered"`
	Transformed int64                     `json:"transformed"`
	Sent        int64                     `json:"sent"`
	Errored     int64                     `json:"errored"`
	Queued      int64                     `json:"queued"`
	Connectors  map[string]ConnectorStats `json:"connectors,omitempty"`
	// LastMessageAt is when the flow last received a message (nil if never).
	LastMessageAt *time.Time `json:"lastMessageAt,omitempty"`
}

// StatsRegistry tracks per-flow current and lifetime statistics with reset
// and dump-to-file (spec §2.11.36).
type StatsRegistry struct {
	mu       sync.Mutex
	current  map[string]*FlowStats
	lifetime map[string]*FlowStats
}

// NewStatsRegistry returns an empty stats registry.
func NewStatsRegistry() *StatsRegistry {
	return &StatsRegistry{
		current:  make(map[string]*FlowStats),
		lifetime: make(map[string]*FlowStats),
	}
}

// Inc increments the current and lifetime counter for a flow.
func (s *StatsRegistry) Inc(flow string, k CounterKind) {
	s.mu.Lock()
	defer s.mu.Unlock()
	apply(s.ensure(s.current, flow), k, 1)
	apply(s.ensure(s.lifetime, flow), k, 1)
}

// Record applies several flow and connector increments for one message under
// a single lock, so a concurrent Snapshot never sees a partial update.
func (s *StatsRegistry) Record(flow string, kinds []CounterKind, connectors map[string]CounterKind) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range []map[string]*FlowStats{s.current, s.lifetime} {
		fs := s.ensure(m, flow)
		for _, k := range kinds {
			apply(fs, k, 1)
		}
		for c, k := range connectors {
			applyConnector(fs, c, k, 1)
		}
	}
}

// IncConnector increments a connector-level counter for a flow.
func (s *StatsRegistry) IncConnector(flow, connector string, k CounterKind) {
	s.mu.Lock()
	defer s.mu.Unlock()
	applyConnector(s.ensure(s.current, flow), connector, k, 1)
	applyConnector(s.ensure(s.lifetime, flow), connector, k, 1)
}

// Snapshot returns a copy of the current (or lifetime) stats for a flow.
func (s *StatsRegistry) Snapshot(flow string, lifetime bool) FlowStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.current
	if lifetime {
		m = s.lifetime
	}
	fs := m[flow]
	if fs == nil {
		return FlowStats{}
	}
	return cloneStats(fs)
}

// Reset clears statistics: a specific flow, or all flows when flow is empty.
func (s *StatsRegistry) Reset(flow string, lifetime bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.current
	if lifetime {
		m = s.lifetime
	}
	if flow == "" {
		for k := range m {
			delete(m, k)
		}
		return
	}
	delete(m, flow)
}

// Clear clears a flow's (every flow's when flow is empty) current counters
// and, with lifetime, its lifetime totals too, under one lock so no message
// is counted in one and not the other.
func (s *StatsRegistry) Clear(flow string, lifetime bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	maps := []map[string]*FlowStats{s.current}
	if lifetime {
		maps = append(maps, s.lifetime)
	}
	for _, m := range maps {
		if flow == "" {
			clear(m)
			continue
		}
		delete(m, flow)
	}
}

// Load replaces every flow's current and lifetime stats (stored ones, at
// startup).
func (s *StatsRegistry) Load(current, lifetime map[string]FlowStats) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current, s.lifetime = loadAll(current), loadAll(lifetime)
}

// loadAll copies stats into a registry map.
func loadAll(stats map[string]FlowStats) map[string]*FlowStats {
	m := make(map[string]*FlowStats, len(stats))
	for flow, fs := range stats {
		c := cloneStats(&fs)
		m[flow] = &c
	}
	return m
}

// copyAll copies a registry map.
func copyAll(m map[string]*FlowStats) map[string]FlowStats {
	out := make(map[string]FlowStats, len(m))
	for flow, fs := range m {
		out[flow] = cloneStats(fs)
	}
	return out
}

// SnapshotAll returns a copy of every flow's current (or lifetime) stats,
// taken at one instant.
func (s *StatsRegistry) SnapshotAll(lifetime bool) map[string]FlowStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	if lifetime {
		return copyAll(s.lifetime)
	}
	return copyAll(s.current)
}

// Snapshots returns a copy of every flow's current and lifetime stats,
// both taken at one instant.
func (s *StatsRegistry) Snapshots() (current, lifetime map[string]FlowStats) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return copyAll(s.current), copyAll(s.lifetime)
}

// Dump writes all flows' statistics to path as JSON (spec §2.11.36).
func (s *StatsRegistry) Dump(path string, lifetime bool) error {
	s.mu.Lock()
	m := s.current
	if lifetime {
		m = s.lifetime
	}
	snap := make(map[string]FlowStats, len(m))
	for k, v := range m {
		snap[k] = cloneStats(v)
	}
	s.mu.Unlock()

	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func (s *StatsRegistry) ensure(m map[string]*FlowStats, flow string) *FlowStats {
	fs := m[flow]
	if fs == nil {
		fs = &FlowStats{}
		m[flow] = fs
	}
	return fs
}

func apply(fs *FlowStats, k CounterKind, delta int64) {
	if k == Received && delta > 0 {
		now := time.Now()
		fs.LastMessageAt = &now
	}
	switch k {
	case Received:
		fs.Received += delta
	case Filtered:
		fs.Filtered += delta
	case Transformed:
		fs.Transformed += delta
	case Sent:
		fs.Sent += delta
	case Errored:
		fs.Errored += delta
	case Queued:
		fs.Queued += delta
	}
}

func applyConnector(fs *FlowStats, connector string, k CounterKind, delta int64) {
	if fs.Connectors == nil {
		fs.Connectors = make(map[string]ConnectorStats)
	}
	cs := fs.Connectors[connector]
	switch k {
	case Received:
		cs.Received += delta
	case Sent:
		cs.Sent += delta
	case Errored:
		cs.Errored += delta
	case Queued:
		cs.Queued += delta
	}
	fs.Connectors[connector] = cs
}

func cloneStats(fs *FlowStats) FlowStats {
	out := *fs
	if fs.Connectors != nil {
		out.Connectors = make(map[string]ConnectorStats, len(fs.Connectors))
		for k, v := range fs.Connectors {
			out.Connectors[k] = v
		}
	}
	return out
}

// TimeSeriesPoint is a single snapshot for trending (spec §2.11.37).
type TimeSeriesPoint struct {
	At    time.Time `json:"at"`
	Flow  string    `json:"flow"`
	Stats FlowStats `json:"stats"`
}

// TimeSeries keeps per-flow snapshots for trending, dropping those older
// than its retention and, past maxPoints in total, the oldest.
type TimeSeries struct {
	mu        sync.Mutex
	points    []TimeSeriesPoint // in recording order
	retention time.Duration
	maxPoints int
}

// NewTimeSeries returns a time series keeping snapshots for retention, at
// most maxPoints in total.
func NewTimeSeries(retention time.Duration, maxPoints int) *TimeSeries {
	return &TimeSeries{retention: retention, maxPoints: maxPoints}
}

// RecordAll appends one snapshot per flow, all stamped with the same
// wall-clock time, and drops snapshots older than the retention before that
// time and any past maxPoints.
func (ts *TimeSeries) RecordAll(at time.Time, stats map[string]FlowStats) {
	at = at.Round(0) // wall clock only, as reported and filtered
	flows := make([]string, 0, len(stats))
	for f := range stats {
		flows = append(flows, f)
	}
	sort.Strings(flows)
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for _, f := range flows {
		ts.points = append(ts.points, TimeSeriesPoint{At: at, Flow: f, Stats: stats[f]})
	}
	cut := max(0, len(ts.points)-ts.maxPoints)
	for cut < len(ts.points) && ts.points[cut].At.Before(at.Add(-ts.retention)) {
		cut++
	}
	ts.points = slices.Clone(ts.points[cut:]) // release the dropped points
}

// Load replaces the snapshots with points (stored ones, at startup), which
// are in recording order.
func (ts *TimeSeries) Load(points []TimeSeriesPoint) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.points = slices.Clone(points[max(0, len(points)-ts.maxPoints):])
}

// Forget drops every snapshot of flow.
func (ts *TimeSeries) Forget(flow string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.points = slices.DeleteFunc(ts.points, func(p TimeSeriesPoint) bool { return p.Flow == flow })
}

// Series returns the newest limit (0 = all) snapshots whose flow satisfies
// keep, taken at or after from and at or before to (zero = open), in
// recording order. Snapshots are recorded in time order, so the search
// stops at the first one before from.
func (ts *TimeSeries) Series(keep func(flow string) bool, from, to time.Time, limit int) []TimeSeriesPoint {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	out := make([]TimeSeriesPoint, 0)
	for i := len(ts.points) - 1; i >= 0 && (limit == 0 || len(out) < limit); i-- {
		p := ts.points[i]
		if !from.IsZero() && p.At.Before(from) {
			break
		}
		if keep(p.Flow) && (to.IsZero() || !p.At.After(to)) {
			out = append(out, p)
		}
	}
	slices.Reverse(out)
	return out
}
