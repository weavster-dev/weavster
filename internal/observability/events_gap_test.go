package observability

import "testing"

// TestEventLogExport covers EventLog.Export, which had no direct test
// (only its underlying Search was exercised). Export is the path the
// events-export API endpoint relies on, so a regression here would silently
// break audit/event exports.
func TestEventLogExport(t *testing.T) {
	l := NewEventLog()
	l.Add("flow.start", "system", "flow-a", nil)
	l.Add("flow.error", "system", "flow-b", nil)

	all := l.Export(EventFilter{})
	if len(all) != 2 {
		t.Fatalf("Export(no filter) = %d events, want 2", len(all))
	}

	filtered := l.Export(EventFilter{Flow: "flow-a"})
	if len(filtered) != 1 || filtered[0].Flow != "flow-a" {
		t.Fatalf("Export(flow filter) = %+v, want single flow-a event", filtered)
	}
}
