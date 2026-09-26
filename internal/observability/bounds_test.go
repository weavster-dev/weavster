package observability

import "testing"

func TestEventLogRingAndLimit(t *testing.T) {
	if got := NewEventLog().Search(EventFilter{Limit: 5}); len(got) != 0 {
		t.Errorf("empty log search = %v", got)
	}
	l := NewEventLog()
	for i := 0; i < MaxEvents+5; i++ {
		l.Add("x", "", "f", nil)
	}
	all := l.Search(EventFilter{})
	if len(all) != MaxEvents || all[0].ID != 6 || all[len(all)-1].ID != MaxEvents+5 || len(l.events) != MaxEvents {
		t.Errorf("kept %d events (ids %d..%d); want exactly the newest %d, oldest first", len(all), all[0].ID, all[len(all)-1].ID, MaxEvents)
	}
	if got := l.Search(EventFilter{Limit: 3}); len(got) != 3 || got[2].ID != MaxEvents+5 {
		t.Errorf("limit 3 = %d events ending at %d; want the newest 3", len(got), got[len(got)-1].ID)
	}
}

func TestStatsRecordAndLastMessageAt(t *testing.T) {
	s := NewStatsRegistry()
	if s.Snapshot("f", false).LastMessageAt != nil {
		t.Error("LastMessageAt set before any message")
	}
	s.Record("f", []CounterKind{Transformed, Sent}, map[string]CounterKind{"ehr": Sent, "archive": Errored})
	got := s.Snapshot("f", true)
	if got.LastMessageAt != nil || got.Transformed != 1 || got.Sent != 1 || got.Connectors["ehr"].Sent != 1 || got.Connectors["archive"].Errored != 1 {
		t.Errorf("Record = %+v", got)
	}
	s.Inc("f", Received)
	if s.Snapshot("f", false).LastMessageAt == nil {
		t.Error("LastMessageAt not set on Received")
	}
}
