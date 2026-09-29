package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/flowdef"
	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/observability"
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
		{gateway.Flow{SourceType: "http"}, "http"},
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

// TestTopologyRecent: counters below the window's first sample (a lifetime
// reset) count from zero, per flow and per destination.
func TestTopologyRecent(t *testing.T) {
	got := since(observability.FlowStats{Sent: 2, Errored: 5, Connectors: map[string]observability.ConnectorStats{"d": {Sent: 1, Errored: 9}}},
		observability.FlowStats{Sent: 10, Errored: 1, Connectors: map[string]observability.ConnectorStats{"d": {Sent: 4, Errored: 3}}})
	if got.Sent != 2 || got.Errored != 4 || got.Connectors["d"] != (observability.ConnectorStats{Sent: 1, Errored: 6}) {
		t.Errorf("since = %+v", got)
	}

	stats := observability.NewStatsRegistry()
	series := observability.NewTimeSeries(time.Hour, 10)
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	series.RecordAll(now.Add(-10*time.Minute), map[string]observability.FlowStats{"a": {}}) // before the window
	ta := topologyAdapter{stats: stats, series: series, now: func() time.Time { return now }}
	if st := ta.state(gateway.Flow{ID: "a", Status: "started"}); st.ok || st.edge(1, 0) != "idle" {
		t.Errorf("no recent sample: %+v", st)
	}
}
