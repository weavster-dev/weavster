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
