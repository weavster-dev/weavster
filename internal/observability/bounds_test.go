package observability

import "testing"

func TestEventLogBoundedAndLastMessageAt(t *testing.T) {
	l := NewEventLog()
	for i := 0; i < MaxEvents+5; i++ {
		l.Add("x", "", "f", nil)
	}
	all := l.Search(EventFilter{})
	if len(all) != MaxEvents || all[0].ID != 6 {
		t.Errorf("kept %d events starting at %d; want the newest %d", len(all), all[0].ID, MaxEvents)
	}

	s := NewStatsRegistry()
	if !s.Snapshot("f", false).LastMessageAt.IsZero() {
		t.Error("LastMessageAt set before any message")
	}
	s.Inc("f", Sent)
	if !s.Snapshot("f", false).LastMessageAt.IsZero() {
		t.Error("LastMessageAt set by a non-Received counter")
	}
	s.Inc("f", Received)
	if s.Snapshot("f", true).LastMessageAt.IsZero() {
		t.Error("LastMessageAt not set on Received")
	}
}
