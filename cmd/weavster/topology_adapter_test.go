package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/flowdef"
	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/observability"
	"github.com/weavster-dev/weavster/internal/topology"
)

// TestSourceLabel: a source is named by where its messages come from,
// never with its secrets.
func TestSourceLabel(t *testing.T) {
	for _, tt := range []struct {
		f    gateway.Flow
		want string
	}{
		{gateway.Flow{Source: &flowdef.Source{Type: "file", Dir: "/in"}}, "file:/in"},
		{gateway.Flow{Source: &flowdef.Source{Type: "file", Dir: "/in", Pattern: "*.hl7"}}, "file:/in (*.hl7)"},
		{gateway.Flow{Source: &flowdef.Source{Type: "http", Address: ":9001", Path: "/adt", CertFile: "/c.pem"}}, "https://:9001/adt"},
		{gateway.Flow{Source: &flowdef.Source{Type: "mllp", Address: ":2575"}}, "mllp://:2575"},
		{gateway.Flow{Source: &flowdef.Source{Type: "database", Driver: "postgres", DSNEnv: "WEAVSTER_DB_HIS"}}, "database:postgres"},
		{gateway.Flow{Source: &flowdef.Source{Type: "other"}}, "other"},
	} {
		if got := sourceLabel(tt.f); got != tt.want {
			t.Errorf("%+v = %q, want %q", tt.f.Source, got, tt.want)
		}
	}
}

// TestTransformSummary: a transform's name and steps; none when absent or
// unreadable.
func TestTransformSummary(t *testing.T) {
	for raw, want := range map[string]struct {
		name  string
		steps int
		ok    bool
	}{
		``:                             {"", 0, false},
		`null`:                         {"", 0, false},
		`[`:                            {"", 0, false},
		`{"steps":[{"map":{}}]}`:       {"transform", 1, true},
		`{"name":"n","steps":[{},{}]}`: {"n", 2, true},
	} {
		name, steps, ok := transformSummary(json.RawMessage(raw))
		if name != want.name || steps != want.steps || ok != want.ok {
			t.Errorf("%q = %q %d %v", raw, name, steps, ok)
		}
	}
}

// TestTopologyRecent: what was counted in the window adds up the increases
// between samples, so a lifetime reset in between counts from zero, per
// flow and per destination; the window starts at the newest sample before
// it.
func TestTopologyRecent(t *testing.T) {
	c := func(sent, errored int64) observability.FlowStats {
		return observability.FlowStats{Sent: sent, Errored: errored, Connectors: map[string]observability.ConnectorStats{"d": {Sent: sent, Errored: errored}}}
	}
	// 10 errors before the window; a reset; then 12 errors.
	got := increase([]observability.FlowStats{c(5, 10), c(0, 0), c(0, 12)})
	if got.Sent != 0 || got.Errored != 12 || got.Connectors["d"] != (observability.ConnectorStats{Errored: 12}) {
		t.Errorf("increase = %+v", got)
	}

	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	stats := observability.NewStatsRegistry()
	series := observability.NewTimeSeries(time.Hour, 10)
	ta := topologyAdapter{stats: stats, series: series, now: func() time.Time { return now }}
	flow := gateway.Flow{ID: "a", Status: "started"}
	if st := ta.snapshot().state(flow); st.ok || st.edge(1, 0) != "idle" {
		t.Errorf("no sample: %+v", st)
	}
	// A sample long before the window (a slow sampleIntervalMs) is the base.
	series.RecordAll(now.Add(-10*time.Minute), map[string]observability.FlowStats{"a": {}})
	stats.IncConnector("a", "d", observability.Errored)
	if st := ta.snapshot().state(flow); !st.ok || st.status(0, st.recent.Connectors["d"].Errored) != "errored" {
		t.Errorf("with an older sample: %+v", st)
	}
	if mergeEdge("idle", "active") != "active" || mergeEdge("", "errored") != "errored" || mergeEdge("", "") != "" {
		t.Error("mergeEdge")
	}
	if a := add(&topology.Activity{Sent: 1}, &topology.Activity{Sent: 2}); a.Sent != 3 || add(nil, nil) != nil {
		t.Errorf("add = %+v", a)
	}
}
