package observability

import (
	"testing"
	"time"
)

// TestStatsLoad: loaded statistics replace the registry's and are copies;
// loaded points replace the series, keeping the newest maxPoints.
func TestStatsLoad(t *testing.T) {
	s := NewStatsRegistry()
	s.Inc("old", Sent)
	in := FlowStats{Sent: 3, Connectors: map[string]ConnectorStats{"out": {Sent: 3}}}
	s.Load(map[string]FlowStats{"adt": in}, map[string]FlowStats{"adt": {Sent: 10}})
	in.Connectors["out"] = ConnectorStats{} // the registry kept its own copy
	if got := s.Snapshot("adt", false); got.Sent != 3 || got.Connectors["out"].Sent != 3 {
		t.Errorf("current = %+v", got)
	}
	if got := s.Snapshot("adt", true); got.Sent != 10 {
		t.Errorf("lifetime = %+v", got)
	}
	if got := s.SnapshotAll(false); len(got) != 1 {
		t.Errorf("the old flow was kept: %+v", got)
	}
	s.Inc("adt", Sent)
	if cur, life := s.Snapshots(); cur["adt"].Sent != 4 || life["adt"].Sent != 11 || len(cur) != 1 {
		t.Errorf("counting on = %+v %+v", cur, life)
	}

	ts := NewTimeSeries(time.Hour, 2)
	at := time.Now()
	ts.Load([]TimeSeriesPoint{{At: at, Flow: "a"}, {At: at, Flow: "b"}, {At: at, Flow: "c"}})
	if got := ts.Series(func(string) bool { return true }, time.Time{}, time.Time{}, 0); len(got) != 2 || got[0].Flow != "b" {
		t.Errorf("series = %+v", got)
	}
}

// TestTimeSeriesRecent: each flow's snapshots from the newest one before
// the window on, found in one pass that stops after the first sampling
// time before it.
func TestTimeSeriesRecent(t *testing.T) {
	ts := NewTimeSeries(24*time.Hour, 100)
	base := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	for i, flows := range [][]string{{"a"}, {"a", "b"}, {"a", "b"}, {"a", "b", "c"}} {
		stats := map[string]FlowStats{}
		for _, f := range flows {
			stats[f] = FlowStats{Sent: int64(i)}
		}
		ts.RecordAll(base.Add(time.Duration(i)*time.Minute), stats)
	}
	got := ts.Recent(base.Add(2*time.Minute + time.Second))
	sent := func(f string) (out []int64) {
		for _, p := range got[f] {
			out = append(out, p.Stats.Sent)
		}
		return out
	}
	if a, b, c := sent("a"), sent("b"), sent("c"); len(a) != 2 || a[0] != 2 || a[1] != 3 || len(b) != 2 || b[0] != 2 || len(c) != 1 || c[0] != 3 {
		t.Errorf("recent = a %v, b %v, c %v", a, b, c)
	}
	if got := ts.Recent(base.Add(time.Hour)); len(got["a"]) != 1 || got["a"][0].Stats.Sent != 3 {
		t.Errorf("after the last sample = %+v", got)
	}
}
