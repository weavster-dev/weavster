package observability

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestStatsResetAllFlows covers the Reset(flow=="") "clear everything" branch,
// which deletes every flow's counters in the selected (current) registry.
func TestStatsResetAllFlows(t *testing.T) {
	s := NewStatsRegistry()
	s.Inc("flow:a", Received)
	s.Inc("flow:b", Sent)

	s.Reset("", false) // reset ALL current flows

	if got := s.Snapshot("flow:a", false); got.Received != 0 {
		t.Errorf("flow:a current after reset-all = %+v, want zeroed", got)
	}
	if got := s.Snapshot("flow:b", false); got.Sent != 0 {
		t.Errorf("flow:b current after reset-all = %+v, want zeroed", got)
	}
	// Lifetime counters must be untouched by a current-only reset.
	if got := s.Snapshot("flow:a", true); got.Received != 1 {
		t.Errorf("flow:a lifetime after current reset = %+v, want retained", got)
	}
}

// TestStatsResetLifetime covers the Reset(flow, lifetime=true) branch, which
// clears the lifetime registry while leaving the current registry intact.
func TestStatsResetLifetime(t *testing.T) {
	s := NewStatsRegistry()
	s.Inc("flow:a", Sent)

	s.Reset("flow:a", true) // reset lifetime only

	if got := s.Snapshot("flow:a", true); got.Sent != 0 {
		t.Errorf("flow:a lifetime after reset = %+v, want zeroed", got)
	}
	if got := s.Snapshot("flow:a", false); got.Sent != 1 {
		t.Errorf("flow:a current after lifetime reset = %+v, want retained", got)
	}
}

// TestStatsApplyConnectorAllKinds exercises every CounterKind branch of the
// unexported applyConnector() switch (stats.go). The existing tests only drive
// connector-level Sent; Received, Errored, and Queued were uncovered.
func TestStatsApplyConnectorAllKinds(t *testing.T) {
	s := NewStatsRegistry()
	s.IncConnector("flow:a", "tcp-1", Received)
	s.IncConnector("flow:a", "tcp-1", Sent)
	s.IncConnector("flow:a", "tcp-1", Errored)
	s.IncConnector("flow:a", "tcp-1", Queued)

	cs := s.Snapshot("flow:a", false).Connectors["tcp-1"]
	if cs.Received != 1 || cs.Sent != 1 || cs.Errored != 1 || cs.Queued != 1 {
		t.Errorf("connector stats = %+v, want one of each counter", cs)
	}
}

// TestStatsDumpLifetime covers Dump's lifetime branch, serializing the
// lifetime registry to disk instead of the current one.
func TestStatsDumpLifetime(t *testing.T) {
	s := NewStatsRegistry()
	s.Inc("flow:a", Received)
	s.Inc("flow:a", Sent)
	s.Inc("flow:b", Errored)

	path := filepath.Join(t.TempDir(), "lifetime.json")
	if err := s.Dump(path, true); err != nil {
		t.Fatalf("dump lifetime: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read dump: %v", err)
	}
	var got map[string]FlowStats
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal dump: %v", err)
	}
	if got["flow:a"].Received != 1 || got["flow:a"].Sent != 1 || got["flow:b"].Errored != 1 {
		t.Errorf("lifetime dump = %+v", got)
	}
}
