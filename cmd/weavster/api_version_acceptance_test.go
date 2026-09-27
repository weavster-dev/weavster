package main

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestUnversionedAPI: /api/... behaves exactly like /api/v1/... through the
// composed server (auth, CSRF marker, audit, handlers, errors).
func TestUnversionedAPI(t *testing.T) {
	c := startComposed(t, serverconfig.Default(), io.Discard)
	admin := basic(bootstrapAdmin, testAdminPassword)
	steps := []struct {
		method, path, body string
		creds              func(*http.Request)
		status             int
		contains, version  string
	}{
		{http.MethodPost, "/api/flows", `{"id":"adt"}`, admin, http.StatusCreated, `"id":"adt"`, "v1"},
		{http.MethodGet, "/api/v1/flows/adt", ``, admin, http.StatusOK, `"status":"undeployed"`, "v1"},
		{http.MethodPost, "/api/flows/adt/deploy", ``, admin, http.StatusOK, `"status":"deployed"`, "v1"},
		{http.MethodGet, "/api/flows", ``, admin, http.StatusOK, `"status":"deployed"`, "v1"},
		{http.MethodGet, "/api/flows", ``, nil, http.StatusUnauthorized, `"code":"UNAUTHORIZED"`, "v1"},
		{http.MethodGet, "/api/flows/zz", ``, admin, http.StatusNotFound, `"code":"NOT_FOUND"`, "v1"},
		{http.MethodGet, "/api/v9/flows", ``, admin, http.StatusNotFound, `"no such endpoint"`, ""},
	}
	for _, s := range steps {
		status, body, hdr := c.do(s.method, s.path, s.body, s.creds)
		if status != s.status || !strings.Contains(body, s.contains) {
			t.Errorf("%s %s: %d %q; want %d containing %q", s.method, s.path, status, body, s.status, s.contains)
		}
		if hdr.Get("Weavster-API-Version") != s.version {
			t.Errorf("%s %s: Weavster-API-Version %q, want %q", s.method, s.path, hdr.Get("Weavster-API-Version"), s.version)
		}
	}
}
