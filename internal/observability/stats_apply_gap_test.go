package observability

import "testing"

// TestStatsRegistryAllCounterKinds exercises every CounterKind branch of the
// unexported apply() switch (stats.go). The existing TestStatsRegistryResetAndDump
// only drives Received and Sent, leaving Filtered, Transformed, Errored, and
// Queued uncovered.
func TestStatsRegistryAllCounterKinds(t *testing.T) {
	s := NewStatsRegistry()
	kinds := []CounterKind{Received, Filtered, Transformed, Sent, Errored, Queued}
	for _, k := range kinds {
		s.Inc("flow:all", k)
	}

	snap := s.Snapshot("flow:all", false)
	if snap.Received != 1 {
		t.Errorf("Received = %d, want 1", snap.Received)
	}
	if snap.Filtered != 1 {
		t.Errorf("Filtered = %d, want 1", snap.Filtered)
	}
	if snap.Transformed != 1 {
		t.Errorf("Transformed = %d, want 1", snap.Transformed)
	}
	if snap.Sent != 1 {
		t.Errorf("Sent = %d, want 1", snap.Sent)
	}
	if snap.Errored != 1 {
		t.Errorf("Errored = %d, want 1", snap.Errored)
	}
	if snap.Queued != 1 {
		t.Errorf("Queued = %d, want 1", snap.Queued)
	}
}

// TestStatsRegistryUnknownCounterKind asserts that an out-of-range CounterKind
// is silently ignored (the switch's implicit default), rather than panicking
// or corrupting other counters.
func TestStatsRegistryUnknownCounterKind(t *testing.T) {
	s := NewStatsRegistry()
	const unknown CounterKind = 999
	s.Inc("flow:unknown", unknown)

	snap := s.Snapshot("flow:unknown", false)
	if snap.Received != 0 || snap.Filtered != 0 || snap.Transformed != 0 ||
		snap.Sent != 0 || snap.Errored != 0 || snap.Queued != 0 {
		t.Errorf("unknown CounterKind mutated stats: %+v", snap)
	}
}
