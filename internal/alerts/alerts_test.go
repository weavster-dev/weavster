package alerts

import (
	"context"
	"testing"

	"github.com/weavster-dev/weavster/internal/notify"
)

type fakeNotifier struct {
	calls []notify.Notification
}

func (f *fakeNotifier) Notify(_ context.Context, n notify.Notification) error {
	f.calls = append(f.calls, n)
	return nil
}

func TestEvaluateAndHandle(t *testing.T) {
	fn := &fakeNotifier{}
	m := NewManager(fn)
	ctx := context.Background()

	m.Add(Alert{ID: "a1", Trigger: "processing-error", Recipients: []string{"ops@example.com"}, Scope: "flow:admit", Enabled: true})
	m.Add(Alert{ID: "a2", Trigger: "processing-error", Recipients: []string{"ops@example.com"}, Scope: "flow:other", Enabled: true})
	m.Add(Alert{ID: "a3", Trigger: "processing-error", Recipients: []string{"ops@example.com"}, Enabled: false})

	matched := m.Evaluate(ProcessingError{Flow: "admit", Err: "timeout"})
	if len(matched) != 1 || matched[0].ID != "a1" {
		t.Errorf("matched = %+v", matched)
	}

	if err := m.Handle(ctx, ProcessingError{Flow: "admit", Err: "timeout"}); err != nil {
		t.Fatal(err)
	}
	if len(fn.calls) != 1 || fn.calls[0].Recipients[0] != "ops@example.com" {
		t.Errorf("notifications = %+v", fn.calls)
	}
}

func TestEnableDisableTest(t *testing.T) {
	fn := &fakeNotifier{}
	m := NewManager(fn)
	ctx := context.Background()

	m.Add(Alert{ID: "a1", Trigger: "processing-error", Recipients: []string{"r@example.com"}, Enabled: true})
	if err := m.Enable("a1", false); err != nil {
		t.Fatal(err)
	}
	if err := m.Handle(ctx, ProcessingError{Flow: "f", Err: "e"}); err != nil {
		t.Fatal(err)
	}
	if len(fn.calls) != 0 {
		t.Error("disabled alert must not fire")
	}

	// Test fires once even while disabled.
	if err := m.Test(ctx, "a1"); err != nil {
		t.Fatal(err)
	}
	if len(fn.calls) != 1 {
		t.Errorf("test fire calls = %d", len(fn.calls))
	}
}

func TestImportExport(t *testing.T) {
	m := NewManager(nil)
	m.Add(Alert{ID: "a1", Trigger: "processing-error", Recipients: []string{"x@example.com"}, Scope: "flow:f", Enabled: true})
	m.Add(Alert{ID: "a2", Trigger: "processing-error", Recipients: []string{"y@example.com"}, Enabled: true})

	data, err := m.Export()
	if err != nil {
		t.Fatal(err)
	}

	m2 := NewManager(nil)
	if err := m2.Import(data); err != nil {
		t.Fatal(err)
	}
	if len(m2.List()) != 2 {
		t.Errorf("imported = %d, want 2", len(m2.List()))
	}
}
