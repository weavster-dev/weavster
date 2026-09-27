package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestErrorEnvelopeComposed: through the composed server (auth, CSRF
// marker, audit, handlers), every error class answers with the JSON error
// envelope and the right status.
func TestErrorEnvelopeComposed(t *testing.T) {
	c := startComposed(t, serverconfig.Default(), io.Discard)
	admin := basic(bootstrapAdmin, testAdminPassword)
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"adt"}`, admin); code != http.StatusCreated {
		t.Fatalf("create: %d %q", code, body)
	}
	noMarker := func(r *http.Request) {
		r.Header.Del("X-Weavster-CSRF")
		r.SetBasicAuth(bootstrapAdmin, testAdminPassword)
	}
	tests := []struct {
		name, method, path, body string
		creds                    func(*http.Request)
		status                   int
		code                     string
	}{
		{"unauthenticated", http.MethodGet, "/api/v1/flows", ``, nil, http.StatusUnauthorized, "UNAUTHORIZED"},
		{"missing marker header", http.MethodGet, "/api/v1/flows", ``, noMarker, http.StatusBadRequest, "BAD_REQUEST"},
		{"unknown route", http.MethodGet, "/api/v1/nope", ``, admin, http.StatusNotFound, "NOT_FOUND"},
		{"wrong method", http.MethodPatch, "/api/v1/flows/adt", ``, admin, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED"},
		{"invalid definition", http.MethodPost, "/api/v1/flows", `{"id":"x","status":"started"}`, admin, http.StatusBadRequest, "BAD_REQUEST"},
		{"unknown flow", http.MethodDelete, "/api/v1/flows/zz", ``, admin, http.StatusNotFound, "NOT_FOUND"},
		{"unknown topology flow", http.MethodGet, "/api/v1/topology/flows/zz", ``, admin, http.StatusNotFound, "NOT_FOUND"},
		{"duplicate flow", http.MethodPost, "/api/v1/flows", `{"id":"adt"}`, admin, http.StatusConflict, "CONFLICT"},
		{"invalid transition", http.MethodPost, "/api/v1/flows/adt/start", ``, admin, http.StatusConflict, "CONFLICT"},
		{"message too large", http.MethodPost, "/api/v1/flows/adt/messages", strings.Repeat("x", 10<<20+1), admin, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body, hdr := c.do(tt.method, tt.path, tt.body, tt.creds)
			var env struct {
				Error struct{ Code, Message string } `json:"error"`
			}
			if err := json.Unmarshal([]byte(body), &env); err != nil || status != tt.status || env.Error.Code != tt.code ||
				env.Error.Message == "" || hdr.Get("Content-Type") != "application/json" {
				t.Errorf("%s %s: %d %q (%s); want %d with code %s", tt.method, tt.path, status, body, hdr.Get("Content-Type"), tt.status, tt.code)
			}
		})
	}
}
