package alerts

import (
	"context"
	"testing"
)

// TestRemove covers Manager.Remove (alerts.go), which had 0% coverage.
// Removing an alert must both drop it from List() and stop it from firing
// on subsequent matching events.
func TestRemove(t *testing.T) {
	fn := &fakeNotifier{}
	m := NewManager(fn)
	ctx := context.Background()

	m.Add(Alert{ID: "a1", Trigger: "processing-error", Recipients: []string{"ops@example.com"}, Enabled: true})
	m.Add(Alert{ID: "a2", Trigger: "processing-error", Recipients: []string{"ops@example.com"}, Enabled: true})

	m.Remove("a1")

	list := m.List()
	if len(list) != 1 || list[0].ID != "a2" {
		t.Errorf("List() after Remove = %+v, want only a2", list)
	}

	if err := m.Handle(ctx, ProcessingError{Flow: "f", Err: "e"}); err != nil {
		t.Fatal(err)
	}
	if len(fn.calls) != 1 {
		t.Errorf("notifications = %+v, want exactly 1 (from remaining alert a2)", fn.calls)
	}

	// Removing an unknown ID is a no-op, not an error/panic.
	m.Remove("does-not-exist")
	if len(m.List()) != 1 {
		t.Errorf("List() after removing unknown id = %+v", m.List())
	}
}
