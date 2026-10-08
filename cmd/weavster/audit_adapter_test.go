package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/audit"
	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/state"
)

// failingAudits cannot store or read audit entries.
type failingAudits struct{}

func (failingAudits) AppendAudit(context.Context, state.AuditRecord) (int64, error) {
	return 0, errors.New("store down")
}

func (failingAudits) SearchAudit(context.Context, state.AuditQuery) ([]state.AuditRecord, error) {
	return nil, errors.New("store down")
}

func (failingAudits) DeleteAuditBefore(context.Context, time.Time) (int, error) {
	return 0, errors.New("store down")
}

// TestAuditAdapter: an entry is stored redacted as it is logged; a store
// that fails is logged and does not fail the recording.
func TestAuditAdapter(t *testing.T) {
	ctx := context.Background()
	mem := state.NewMemStore()
	var logs bytes.Buffer
	a := auditAdapter{s: audit.NewLocalSink(slog.New(slog.NewTextHandler(&logs, nil))), repo: mem, logger: slog.New(slog.NewTextHandler(&logs, nil))}
	if err := a.Record(ctx, gateway.AuditEvent{Actor: "admin", Action: "phi.access", Resource: "/api/v1/messages", Detail: map[string]string{"query.apiToken": "s3cr3t"}}); err != nil {
		t.Fatal(err)
	}
	got, err := a.SearchAudit(ctx, gateway.AuditQuery{})
	if err != nil || len(got) != 1 || got[0].Detail["query.apiToken"] != "[redacted]" || strings.Contains(logs.String(), "s3cr3t") {
		t.Errorf("stored %+v %v; log %s", got, err, logs.String())
	}

	logs.Reset()
	broken := auditAdapter{s: audit.NewLocalSink(slog.New(slog.NewTextHandler(&logs, nil))), repo: failingAudits{}, logger: slog.New(slog.NewTextHandler(&logs, nil))}
	if err := broken.Record(ctx, gateway.AuditEvent{Actor: "admin", Action: "x"}); err != nil {
		t.Errorf("Record with a failing store = %v, want nil", err)
	}
	if !strings.Contains(logs.String(), "audit entry not stored") {
		t.Errorf("log = %s", logs.String())
	}
	if _, err := broken.SearchAudit(ctx, gateway.AuditQuery{}); err == nil {
		t.Error("SearchAudit with a failing store = nil error")
	}
}

// TestAuditAdapterSearch: the log line carries the stored id; searches see
// only settled entries, and leave out audit reads unless asked for them.
func TestAuditAdapterSearch(t *testing.T) {
	ctx := context.Background()
	mem := state.NewMemStore()
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	now := time.Now()
	a := auditAdapter{s: audit.NewLocalSink(logger), repo: mem, logger: logger, settle: time.Minute, now: func() time.Time { return now.Add(2 * time.Minute) }}
	for _, action := range []string{"phi.access", gateway.AuditRead} {
		if err := a.Record(ctx, gateway.AuditEvent{Actor: "admin", Action: action}); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(logs.String(), "id=1 ") || !strings.Contains(logs.String(), "id=2 ") {
		t.Errorf("log lines without the stored ids: %s", logs.String())
	}
	for _, tt := range []struct {
		name string
		q    gateway.AuditQuery
		want int
	}{
		{"settled, reads left out", gateway.AuditQuery{OmitReads: true}, 1},
		{"reads asked for", gateway.AuditQuery{Action: gateway.AuditRead}, 1},
		{"everything", gateway.AuditQuery{}, 2},
		{"a later to is capped", gateway.AuditQuery{To: now.Add(time.Hour)}, 2},
	} {
		if got, err := a.SearchAudit(ctx, tt.q); err != nil || len(got) != tt.want {
			t.Errorf("%s: %d entries (%v), want %d", tt.name, len(got), err, tt.want)
		}
	}
	a.now = func() time.Time { return now } // entries younger than the settle time
	if got, _ := a.SearchAudit(ctx, gateway.AuditQuery{}); len(got) != 0 {
		t.Errorf("unsettled entries returned: %+v", got)
	}
}
