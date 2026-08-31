package alerts

import (
	"context"
	"errors"
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

type failingNotifier struct{ err error }

func (f failingNotifier) Notify(context.Context, notify.Notification) error {
	return f.err
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

func TestAlertRemove(t *testing.T) {
	m := NewManager(nil)
	m.Add(Alert{ID: "a1", Trigger: "processing-error", Enabled: true})
	m.Remove("a1")
	if len(m.List()) != 0 {
		t.Error("alert should be removed")
	}
	// Removing non-existent should not panic.
	m.Remove("noexist")
}

func TestEvaluateScopeMatches(t *testing.T) {
	m := NewManager(nil)
	m.Add(Alert{ID: "flow-scoped", Trigger: "processing-error", Scope: "flow:myflow", Enabled: true})
	m.Add(Alert{ID: "source-scoped", Trigger: "processing-error", Scope: "source:mysrc", Enabled: true})
	m.Add(Alert{ID: "no-scope", Trigger: "processing-error", Enabled: true})

	// flow match
	matched := m.Evaluate(ProcessingError{Flow: "myflow", Source: "other", Err: "processing-error"})
	ids := make(map[string]bool)
	for _, a := range matched {
		ids[a.ID] = true
	}
	if !ids["flow-scoped"] {
		t.Error("flow-scoped alert should match")
	}
	if ids["source-scoped"] {
		t.Error("source-scoped alert should not match on different source")
	}
	if !ids["no-scope"] {
		t.Error("no-scope alert should always match")
	}

	// source match
	matched2 := m.Evaluate(ProcessingError{Flow: "other", Source: "mysrc", Err: "processing-error"})
	ids2 := make(map[string]bool)
	for _, a := range matched2 {
		ids2[a.ID] = true
	}
	if !ids2["source-scoped"] {
		t.Error("source-scoped alert should match on its source")
	}
}

func TestHandleDeliver(t *testing.T) {
	fn := &fakeNotifier{}
	m := NewManager(fn)
	m.Add(Alert{ID: "a1", Trigger: "processing-error", Recipients: []string{"x@example.com"}, Enabled: true})

	if err := m.Handle(context.Background(), ProcessingError{Flow: "f1", Err: "processing-error"}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(fn.calls) != 1 {
		t.Errorf("expected 1 notification, got %d", len(fn.calls))
	}
}

func TestHandleNoNotifier(t *testing.T) {
	m := NewManager(nil)
	m.Add(Alert{ID: "a1", Trigger: "processing-error", Enabled: true})
	if err := m.Handle(context.Background(), ProcessingError{Err: "processing-error"}); err != nil {
		t.Fatalf("Handle with nil notifier: %v", err)
	}
}

func TestHandleReturnsNotifierErrorWithAlertID(t *testing.T) {
	notifyErr := errors.New("webhook unavailable")
	m := NewManager(failingNotifier{err: notifyErr})
	m.Add(Alert{ID: "critical-alert", Trigger: "processing-error", Enabled: true})

	err := m.Handle(context.Background(), ProcessingError{Flow: "orders", Err: "timeout"})
	if err == nil {
		t.Fatal("Handle() error = nil, want notifier error")
	}
	if !errors.Is(err, notifyErr) {
		t.Errorf("Handle() error = %v, want wrapped notifier error", err)
	}
	if got, want := err.Error(), "alerts: deliver critical-alert: webhook unavailable"; got != want {
		t.Errorf("Handle() error = %q, want %q", got, want)
	}
}
