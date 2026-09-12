package alerts

import (
	"context"
	"strings"
	"testing"
)

func TestUnknownAlertOperationsReturnErrors(t *testing.T) {
	fn := &fakeNotifier{}
	m := NewManager(fn)

	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "enable",
			run:  func() error { return m.Enable("missing", true) },
		},
		{
			name: "test",
			run:  func() error { return m.Test(context.Background(), "missing") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			if err == nil {
				t.Fatal("error = nil, want missing-alert error")
			}
			if !strings.Contains(err.Error(), "missing") {
				t.Errorf("error = %q, want alert ID", err)
			}
		})
	}

	if len(fn.calls) != 0 {
		t.Errorf("notifications = %d, want 0", len(fn.calls))
	}
}

func TestImportRejectsMalformedJSONWithoutReplacingAlerts(t *testing.T) {
	m := NewManager(nil)
	m.Add(Alert{ID: "existing", Enabled: true})

	if err := m.Import([]byte(`[{"id":"replacement"}`)); err == nil {
		t.Fatal("Import() error = nil, want malformed JSON error")
	}

	alerts := m.List()
	if len(alerts) != 1 || alerts[0].ID != "existing" {
		t.Fatalf("alerts after failed import = %+v, want existing alert unchanged", alerts)
	}
}
