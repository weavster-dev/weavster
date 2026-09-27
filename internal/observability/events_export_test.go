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

func TestEventLogFiltersGetAndMaxID(t *testing.T) {
	l := NewEventLog()
	if l.MaxID() != 0 {
		t.Error("empty log max id")
	}
	a := l.Add("flow.deployed", "", "a", nil)
	b := l.Add("message.sent", "", "a", nil)
	l.Add("message.sent", "", "b", nil)
	if got := l.Search(EventFilter{AfterID: a.ID}); len(got) != 2 || got[0].ID != b.ID {
		t.Errorf("afterId = %+v", got)
	}
	if got := l.Count(EventFilter{Until: a.At}); got < 1 {
		t.Errorf("until = %d", got)
	}
	if got := l.Count(EventFilter{Until: a.At.Add(-time.Hour)}); got != 0 {
		t.Errorf("until before all = %d", got)
	}
	if e, ok := l.Get(b.ID); !ok || e.Type != "message.sent" {
		t.Errorf("get = %+v %v", e, ok)
	}
	if _, ok := l.Get(99); ok {
		t.Error("get unknown")
	}
	if l.MaxID() != 3 {
		t.Errorf("max id = %d", l.MaxID())
	}
}
