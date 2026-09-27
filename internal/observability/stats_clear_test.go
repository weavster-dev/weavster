package observability

import "testing"

func TestClearAndSnapshotAll(t *testing.T) {
	tests := []struct {
		name             string
		flow             string
		lifetime         bool
		wantA, wantALife int64
		wantB, wantBLife int64
	}{
		{"one flow, current", "a", false, 0, 1, 1, 1},
		{"one flow, current and lifetime", "a", true, 0, 0, 1, 1},
		{"every flow, current", "", false, 0, 1, 0, 1},
		{"every flow, current and lifetime", "", true, 0, 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStatsRegistry()
			s.Inc("a", Received)
			s.Inc("b", Received)
			s.Clear(tt.flow, tt.lifetime)
			cur, life := s.SnapshotAll(false), s.SnapshotAll(true)
			if cur["a"].Received != tt.wantA || life["a"].Received != tt.wantALife ||
				cur["b"].Received != tt.wantB || life["b"].Received != tt.wantBLife {
				t.Errorf("current %+v, lifetime %+v", cur, life)
			}
		})
	}
}
