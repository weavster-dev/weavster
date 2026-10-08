package observability

import (
	"path/filepath"
	"testing"
)

func TestStatsRegistryDumpPropagatesWriteError(t *testing.T) {
	stats := NewStatsRegistry()
	stats.Inc("flow:a", Received)

	path := filepath.Join(t.TempDir(), "missing", "stats.json")
	if err := stats.Dump(path, false); err == nil {
		t.Fatal("Dump() error = nil, want write error for missing parent directory")
	}
}
