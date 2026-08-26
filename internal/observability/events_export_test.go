package observability

import (
	"testing"
	"time"
)

// TestEventLogExport covers EventLog.Export, which previously had 0%
// coverage. Export is the read path used by the admin/audit API to download
// filtered event history, so it must honor EventFilter the same way Search
// does.
func TestEventLogExport(t *testing.T) {
	log := NewEventLog()
	log.Add("flow.start", "system", "admit", nil)
	log.Add("flow.error", "system", "admit", nil)
	log.Add("flow.start", "system", "discharge", nil)

	got := log.Export(EventFilter{Flow: "admit"})
	if len(got) != 2 {
		t.Fatalf("Export(flow=admit) len = %d, want 2", len(got))
	}
	for _, e := range got {
		if e.Flow != "admit" {
			t.Errorf("Export returned event with flow %q, want %q", e.Flow, "admit")
		}
	}

	got = log.Export(EventFilter{Type: "flow.error"})
	if len(got) != 1 || got[0].Type != "flow.error" {
		t.Fatalf("Export(type=flow.error) = %+v, want single flow.error event", got)
	}

	got = log.Export(EventFilter{Since: time.Now().Add(time.Hour)})
	if len(got) != 0 {
		t.Fatalf("Export(since=future) len = %d, want 0", len(got))
	}
}
