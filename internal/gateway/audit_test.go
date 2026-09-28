package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
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
		{"login malformed", http.MethodPost, "/api/v1/auth/login", `not json`, nil, AuditLogin, "", "400"},
		{"bad bearer", http.MethodGet, "/api/v1/flows", "", func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") }, AuditAuthFailure, "", "401"},
		{"no credentials", http.MethodDelete, "/api/v1/flows/a", "", nil, AuditAuthFailure, "", "401"},
		{"password change required", http.MethodPost, "/api/v1/flows", `{"id":"a"}`, func(r *http.Request) { r.SetBasicAuth("fresh", "pw") }, "POST /api/v1/flows", "fresh", "403"},
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

func TestAuditRecordsMarkerRejections(t *testing.T) {
	sink := &captureSink{}
	s := newAuthServer(fakePasswords{})
	s.cfg.Audit, s.cfg.RequireCSRF = sink, true
	s = New(s.cfg)
	serve(s, http.MethodPost, "/api/v1/auth/login", `{"username":"viewer","password":"pw"}`, nil)
	serve(s, http.MethodDelete, "/api/v1/flows/a", "", nil)
	if len(sink.events) != 2 || sink.events[0].Action != "POST /api/v1/auth/login" || sink.events[0].Detail["status"] != "400" ||
		sink.events[1].Action != "DELETE /api/v1/flows/a" || sink.events[1].Detail["status"] != "400" {
		t.Errorf("events = %+v; want the login and delete recorded with status 400", sink.events)
	}
}

func TestAuditQueryDetail(t *testing.T) {
	sink := &captureSink{}
	s := New(Config{Audit: sink})
	serve(s, http.MethodGet, "/api/v1/messages?status=sent&status=queued&Token=abc", "", nil)
	if len(sink.events) != 1 || sink.events[0].Action != AuditPHIAccess ||
		sink.events[0].Detail["query.status"] != "sent,queued" || sink.events[0].Detail["query.Token"] != "abc" {
		t.Errorf("events = %+v; the gateway passes query parameters for the sink to redact", sink.events)
	}
}

// TestAuditRecordsRequestedPath: an unversioned call is audited with the
// path the client called, and a rejected one with that literal path.
func TestAuditRecordsRequestedPath(t *testing.T) {
	sink := &captureSink{}
	s := newAuthServer(fakePasswords{})
	s.cfg.Audit, s.cfg.RequireCSRF = sink, true
	s = New(s.cfg)
	serve(s, http.MethodDelete, "/api/flows/a", "", nil)
	if len(sink.events) != 1 || sink.events[0].Action != "DELETE /api/flows/a" || sink.events[0].Resource != "/api/flows/a" {
		t.Errorf("events = %+v; want the unversioned path recorded", sink.events)
	}
}

func TestAuditUnversionedRouteAction(t *testing.T) {
	sink := &captureSink{}
	s := newAuthServer(fakePasswords{})
	s.cfg.Audit = sink
	s = New(s.cfg)
	serve(s, http.MethodDelete, "/api/flows/a", "", func(r *http.Request) { r.SetBasicAuth("viewer", "pw") })
	if len(sink.events) != 1 || sink.events[0].Action != "DELETE /api/flows/{id}" || sink.events[0].Resource != "/api/flows/a" {
		t.Errorf("events = %+v; want the unversioned route recorded", sink.events)
	}
}

// TestAuditDisclosed: a read's audit record names how many messages it
// disclosed and every id, as a JSON array (an id with a comma stays one).
func TestAuditDisclosed(t *testing.T) {
	for _, tt := range []struct {
		name      string
		ids       []string
		count     string
		idsDetail string
	}{
		{"none", []string{}, "0", `[]`},
		{"two", []string{"m0", "m1"}, "2", `["m0","m1"]`},
		{"an id with a comma", []string{"a1,b2"}, "1", `["a1,b2"]`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			info := &auditInfo{}
			r := httptest.NewRequest(http.MethodGet, "/api/v1/messages", nil)
			r = r.WithContext(context.WithValue(r.Context(), auditKey{}, info))
			New(Config{}).auditDisclosed(r, tt.ids)
			if info.detail["messages"] != tt.count || info.detail["messages.ids"] != tt.idsDetail {
				t.Errorf("detail = %v, want messages %s and messages.ids %s", info.detail, tt.count, tt.idsDetail)
			}
		})
	}
}
