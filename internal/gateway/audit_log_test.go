package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeAuditLog records the query, or fails for actor=broken.
type fakeAuditLog struct{ got *AuditQuery }

func (f fakeAuditLog) SearchAudit(_ context.Context, q AuditQuery) ([]AuditEntry, error) {
	*f.got = q
	if q.Actor == "broken" {
		return nil, errors.New("store down")
	}
	return []AuditEntry{{ID: 7, Actor: "admin", Action: AuditPHIAccess}}, nil
}

func TestAuditSearchHandler(t *testing.T) {
	var got AuditQuery
	cfg := Config{AuditLog: fakeAuditLog{got: &got}}
	for _, tt := range []struct {
		name, path string
		cfg        Config
		status     int
		body       string
	}{
		{"search", "/api/v1/audit?actor=admin&action=phi.access&resource=%2Fapi%2Fv1%2Fmessages&from=2026-09-28T00:00:00Z&afterId=3&limit=10", cfg, http.StatusOK, `"id":7`},
		{"bad afterId", "/api/v1/audit?afterId=-1", cfg, http.StatusBadRequest, "afterId must be a whole number"},
		{"bad limit", "/api/v1/audit?limit=0", cfg, http.StatusBadRequest, "between 1 and 1000"},
		{"bad from", "/api/v1/audit?from=yesterday", cfg, http.StatusBadRequest, "RFC 3339"},
		{"store fails", "/api/v1/audit?actor=broken", cfg, http.StatusInternalServerError, ""},
		{"unavailable", "/api/v1/audit", Config{}, http.StatusServiceUnavailable, "audit log unavailable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(tt.cfg).Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.body) {
				t.Errorf("got %d %s; want %d containing %s", rec.Code, rec.Body.String(), tt.status, tt.body)
			}
		})
	}
	New(cfg).Router().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet,
		"/api/v1/audit?actor=admin&action=phi.access&resource=%2Fapi%2Fv1%2Fmessages&afterId=3&limit=10", nil))
	if got.Actor != "admin" || got.Action != AuditPHIAccess || got.Resource != "/api/v1/messages" || got.AfterID != 3 || got.Limit != 10 {
		t.Errorf("query = %+v", got)
	}
}
