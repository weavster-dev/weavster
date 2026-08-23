package observability

import (
	"encoding/json"
	"os"
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

// TimeSeries is a bounded ring of per-flow snapshots for trending.
type TimeSeries struct {
	mu     sync.Mutex
	points []TimeSeriesPoint
	limit  int
}

// NewTimeSeries returns a time-series ring holding at most limit points.
func NewTimeSeries(limit int) *TimeSeries {
	return &TimeSeries{limit: limit}
}

// Record appends a snapshot.
func (ts *TimeSeries) Record(flow string, s FlowStats) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.points = append(ts.points, TimeSeriesPoint{At: time.Now(), Flow: flow, Stats: s})
	if len(ts.points) > ts.limit {
		ts.points = ts.points[len(ts.points)-ts.limit:]
	}
}

// Series returns snapshots for a flow (or all flows when flow is empty).
func (ts *TimeSeries) Series(flow string) []TimeSeriesPoint {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	out := make([]TimeSeriesPoint, 0)
	for _, p := range ts.points {
		if flow == "" || p.Flow == flow {
			out = append(out, p)
		}
	}
	return out
}
