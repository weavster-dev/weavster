package gateway

import (
	"context"
	"net/http"
	"sync"
	"testing"
)

type captureSink struct {
	mu     sync.Mutex
	events []AuditEvent
}

func (c *captureSink) Record(_ context.Context, e AuditEvent) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
	return nil
}

func TestAuditMiddleware(t *testing.T) {
	viewer := func(r *http.Request) { r.SetBasicAuth("viewer", "pw") }
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		set        func(*http.Request)
		wantAction string // "" = no audit entry
		wantActor  string
		wantStatus string
	}{
		{"read is not audited", http.MethodGet, "/api/v1/flows", "", viewer, "", "", ""},
		{"rejected mutation", http.MethodPost, "/api/v1/flows", `{"id":"a"}`, viewer, "POST /api/v1/flows", "viewer", "403"},
		{"delete", http.MethodDelete, "/api/v1/flows/a", "", viewer, "DELETE /api/v1/flows/{id}", "viewer", "403"},
		{"phi access", http.MethodGet, "/api/v1/messages?Token=t&status=sent", "", viewer, AuditPHIAccess, "viewer", "403"},
		{"bad basic", http.MethodGet, "/api/v1/flows", "", func(r *http.Request) { r.SetBasicAuth("viewer", "no") }, AuditAuthFailure, "viewer", "401"},
		{"login ok", http.MethodPost, "/api/v1/auth/login", `{"username":"viewer","password":"pw"}`, nil, AuditLogin, "viewer", "200"},
		{"login fail", http.MethodPost, "/api/v1/auth/login", `{"username":"viewer","password":"no"}`, nil, AuditLogin, "viewer", "401"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &captureSink{}
			s := newAuthServer(fakePasswords{})
			s.cfg.Audit = sink
			serve(s, tt.method, tt.path, tt.body, tt.set)
			if tt.wantAction == "" {
				if len(sink.events) != 0 {
					t.Errorf("unexpected audit events %+v", sink.events)
				}
				return
			}
			if len(sink.events) != 1 {
				t.Fatalf("events = %+v, want 1", sink.events)
			}
			e := sink.events[0]
			if e.Action != tt.wantAction || e.Actor != tt.wantActor || e.Detail["status"] != tt.wantStatus {
				t.Errorf("event = %+v, want action %q actor %q status %s", e, tt.wantAction, tt.wantActor, tt.wantStatus)
			}
		})
	}
}

func TestAuditQueryDetail(t *testing.T) {
	sink := &captureSink{}
	s := New(Config{Audit: sink})
	serve(s, http.MethodGet, "/api/v1/messages?status=sent&Token=abc", "", nil)
	if len(sink.events) != 1 || sink.events[0].Detail["query.status"] != "sent" || sink.events[0].Detail["query.Token"] != "abc" {
		t.Errorf("events = %+v; the gateway passes query parameters for the sink to redact", sink.events)
	}
}
