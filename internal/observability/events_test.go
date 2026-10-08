package observability

import "testing"

// TestEventLogLoadAndSink: loaded events (with gaps in their ids) are kept
// and found, new ids continue after the stored maximum, and every added
// event reaches the sink.
func TestEventLogLoadAndSink(t *testing.T) {
	l := NewEventLog()
	var sunk []Event
	l.SetSink(func(e Event) { sunk = append(sunk, e) })
	l.Load([]Event{{ID: 3, Type: "a"}, {ID: 7, Type: "b"}, {ID: 9, Type: "c"}}, 12)
	for id, want := range map[int64]bool{3: true, 7: true, 9: true, 4: false, 12: false} {
		if _, ok := l.Get(id); ok != want {
			t.Errorf("Get(%d) = %v, want %v", id, ok, want)
		}
	}
	e := l.Add("d", "", "f", nil)
	if e.ID != 13 || len(sunk) != 1 || sunk[0].ID != 13 || l.MaxID() != 13 {
		t.Errorf("added %+v, sunk %+v, max %d", e, sunk, l.MaxID())
	}
	if got, ok := l.Get(13); !ok || got.Type != "d" {
		t.Errorf("Get(13) = %+v %v", got, ok)
	}

	many := make([]Event, MaxEvents+5)
	for i := range many {
		many[i] = Event{ID: int64(i + 1)}
	}
	l.Load(many, int64(len(many)))
	if n := l.Count(EventFilter{}); n != MaxEvents {
		t.Errorf("kept %d, want %d", n, MaxEvents)
	}
	if _, ok := l.Get(5); ok {
		t.Error("an event beyond MaxEvents was kept")
	}
	if _, ok := l.Get(int64(len(many))); !ok {
		t.Error("the newest loaded event is missing")
	}
}
