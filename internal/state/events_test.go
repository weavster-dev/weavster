package state

import (
	"context"
	"testing"
	"time"
)

// eventStore is a backend that keeps events.
type eventStore interface {
	AppendEvents(ctx context.Context, events []EventRecord) error
	RecentEvents(ctx context.Context, n int) ([]EventRecord, int64, error)
	DeleteEventsBefore(ctx context.Context, t time.Time) (int, error)
}

// TestEvents: events are stored with their ids and data, an id stored
// twice is kept once, the newest are read back oldest first with the
// largest id, and old ones can be deleted, on every backend.
func TestEvents(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	for name, s := range testBackends(t) {
		es := s.(eventStore)
		if got, maxID, err := es.RecentEvents(ctx, 10); err != nil || len(got) != 0 || maxID != 0 {
			t.Fatalf("%s: empty store = %v %d %v", name, got, maxID, err)
		}
		batch := []EventRecord{
			{ID: 3, At: base, Type: "message.sent", Flow: "adt", Data: map[string]string{"messageId": "m1"}},
			{ID: 5, At: base.Add(time.Hour), Type: "source.file.rejected", Flow: "adt", Data: map[string]string{"file": "a\x00b"}},
			{ID: 6, At: base.Add(2 * time.Hour), Type: "messages.pruned"},
		}
		if err := es.AppendEvents(ctx, batch); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := es.AppendEvents(ctx, batch[2:]); err != nil { // stored again: kept once
			t.Fatalf("%s: %v", name, err)
		}
		got, maxID, err := es.RecentEvents(ctx, 2)
		if err != nil || maxID != 6 || len(got) != 2 || got[0].ID != 5 || got[1].ID != 6 ||
			got[0].Data["file"] != "a\x00b" || !got[0].At.Equal(base.Add(time.Hour)) || got[1].Data == nil {
			t.Errorf("%s: recent = %+v %d %v", name, got, maxID, err)
		}
		if n, err := es.DeleteEventsBefore(ctx, base.Add(time.Minute)); err != nil || n != 1 {
			t.Errorf("%s: deleted %d %v, want 1", name, n, err)
		}
		if all, _, _ := es.RecentEvents(ctx, 10); len(all) != 2 {
			t.Errorf("%s: left %+v", name, all)
		}
	}
}

// TestMemEventsCap: the memory store keeps the newest maxMemEvents events.
func TestMemEventsCap(t *testing.T) {
	s := NewMemStore()
	batch := make([]EventRecord, maxMemEvents+3)
	for i := range batch {
		batch[i] = EventRecord{ID: int64(i + 1), At: time.Now()}
	}
	if err := s.AppendEvents(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	got, maxID, _ := s.RecentEvents(context.Background(), maxMemEvents+10)
	if len(got) != maxMemEvents || got[0].ID != 4 || maxID != int64(len(batch)) {
		t.Errorf("kept %d from %d, max %d", len(got), got[0].ID, maxID)
	}
}

// TestMemEventsOutOfOrder: events arriving out of id order are all kept, in
// id order.
func TestMemEventsOutOfOrder(t *testing.T) {
	s := NewMemStore()
	ctx := context.Background()
	for _, id := range []int64{6, 5, 7, 5} {
		if err := s.AppendEvents(ctx, []EventRecord{{ID: id, At: time.Now()}}); err != nil {
			t.Fatal(err)
		}
	}
	got, maxID, _ := s.RecentEvents(ctx, 10)
	if len(got) != 3 || got[0].ID != 5 || got[2].ID != 7 || maxID != 7 {
		t.Errorf("events %+v, max %d", got, maxID)
	}
}
