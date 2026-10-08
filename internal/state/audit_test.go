package state

import (
	"context"
	"testing"
	"time"
)

// auditStore is a backend that keeps audit entries.
type auditStore interface {
	AppendAudit(ctx context.Context, r AuditRecord) (int64, error)
	SearchAudit(ctx context.Context, q AuditQuery) ([]AuditRecord, error)
}

// TestAudit: entries are kept with their detail and found by actor,
// action, resource, time, and cursor, oldest first, on every backend.
func TestAudit(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	for name, s := range testBackends(t) {
		a := s.(auditStore)
		for i, r := range []AuditRecord{
			{At: base, Actor: "admin", Action: "POST /api/v1/flows", Resource: "/api/v1/flows", Detail: map[string]string{"status": "201"}},
			{At: base.Add(time.Minute), Actor: "alice", Action: "phi.access", Resource: "/api/v1/messages", Detail: map[string]string{"messages": "2", "messages.ids": `["a","b"]`}},
			{At: base.Add(2 * time.Minute), Actor: "admin", Action: "phi.access", Resource: "/api/v1/messages/a/content", Detail: map[string]string{"part": "raw\x00"}},
		} {
			if id, err := a.AppendAudit(ctx, r); err != nil || id != int64(i+1) {
				t.Fatalf("%s: append %d = %d %v", name, i, id, err)
			}
		}
		for _, tt := range []struct {
			name string
			q    AuditQuery
			ids  []int64
		}{
			{"all", AuditQuery{}, []int64{1, 2, 3}},
			{"by actor", AuditQuery{Actor: "admin"}, []int64{1, 3}},
			{"by action", AuditQuery{Action: "phi.access"}, []int64{2, 3}},
			{"by resource", AuditQuery{Resource: "/api/v1/messages"}, []int64{2}},
			{"from", AuditQuery{From: base.Add(time.Minute)}, []int64{2, 3}},
			{"to", AuditQuery{To: base.Add(time.Minute)}, []int64{1, 2}},
			{"after a cursor", AuditQuery{AfterID: 1, Limit: 1}, []int64{2}},
			{"none", AuditQuery{Actor: "nobody"}, []int64{}},
			{"excluding an action", AuditQuery{ExcludeAction: "phi.access"}, []int64{1}},
			{"from, to the millisecond", AuditQuery{From: base.Add(time.Minute + 900*time.Microsecond)}, []int64{2, 3}},
		} {
			got, err := a.SearchAudit(ctx, tt.q)
			if err != nil {
				t.Fatalf("%s/%s: %v", name, tt.name, err)
			}
			ids := make([]int64, len(got))
			for i, r := range got {
				ids[i] = r.ID
			}
			if len(ids) != len(tt.ids) || (len(ids) > 0 && ids[0] != tt.ids[0]) || (len(ids) > 1 && ids[len(ids)-1] != tt.ids[len(tt.ids)-1]) {
				t.Errorf("%s/%s: ids %v, want %v", name, tt.name, ids, tt.ids)
			}
		}
		got, _ := a.SearchAudit(ctx, AuditQuery{AfterID: 1, Limit: 1})
		if len(got) != 1 || got[0].Detail["messages.ids"] != `["a","b"]` || !got[0].At.Equal(base.Add(time.Minute)) || got[0].Actor != "alice" {
			t.Errorf("%s: entry 2 = %+v", name, got)
		}
	}
}

// TestMemAuditCap: the memory store keeps the newest maxMemAudit entries.
func TestMemAuditCap(t *testing.T) {
	s := NewMemStore()
	ctx := context.Background()
	for range maxMemAudit + 5 {
		if _, err := s.AppendAudit(ctx, AuditRecord{At: time.Now(), Actor: "a", Action: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := s.SearchAudit(ctx, AuditQuery{Limit: 1})
	if len(s.audit) != maxMemAudit || len(got) != 1 || got[0].ID != 6 {
		t.Errorf("kept %d, first %+v; want %d from id 6", len(s.audit), got, maxMemAudit)
	}
}

// TestAuditParity: a nil detail is stored as {}, text is stored as the
// message store keeps it, and old entries can be deleted, on every backend.
func TestAuditParity(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	for name, s := range testBackends(t) {
		a := s.(interface {
			auditStore
			DeleteAuditBefore(ctx context.Context, t time.Time) (int, error)
		})
		if _, err := a.AppendAudit(ctx, AuditRecord{At: base, Actor: "ad\x00min", Action: "x"}); err != nil {
			t.Fatal(err)
		}
		if _, err := a.AppendAudit(ctx, AuditRecord{At: base.Add(time.Hour), Actor: "b", Action: "y", Detail: map[string]string{"k": "v"}}); err != nil {
			t.Fatal(err)
		}
		got, _ := a.SearchAudit(ctx, AuditQuery{Limit: 1})
		if len(got) != 1 || got[0].Detail == nil || got[0].Actor != "admin" {
			t.Errorf("%s: first entry = %+v", name, got)
		}
		if n, err := a.DeleteAuditBefore(ctx, base.Add(time.Minute)); err != nil || n != 1 {
			t.Errorf("%s: deleted %d (%v), want 1", name, n, err)
		}
		if left, _ := a.SearchAudit(ctx, AuditQuery{}); len(left) != 1 || left[0].Actor != "b" {
			t.Errorf("%s: left %+v", name, left)
		}
	}
}
