package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/enterprise"
)

// notImplementedFlows answers every call with the D-17 sentinel.
type notImplementedFlows struct{ errFlows }

func (notImplementedFlows) List(context.Context) ([]Flow, error) {
	return nil, fmt.Errorf("%w: flow federation", enterprise.ErrNotImplemented)
}

// TestErrorEnvelope: every error reply is the JSON envelope with the
// status's code, and internal errors do not leak detail.
func TestErrorEnvelope(t *testing.T) {
	tests := []struct {
		name, method, path, body string
		cfg                      Config
		status                   int
		code, message            string
	}{
		{"unknown route", http.MethodGet, "/api/v1/nope", ``, Config{}, http.StatusNotFound, "NOT_FOUND", "no such endpoint"},
		{"unknown top-level route", http.MethodGet, "/nope", ``, Config{}, http.StatusNotFound, "NOT_FOUND", "no such endpoint"},
		{"wrong method", http.MethodPatch, "/api/v1/flows", ``, Config{}, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed"},
		{"trace blocked", http.MethodTrace, "/api/v1/flows", ``, Config{}, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed"},
		{"unavailable", http.MethodGet, "/api/v1/flows", ``, Config{}, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "flows unavailable"},
		{"unknown destination action", http.MethodPost, "/api/v1/flows/f/destinations/d/restart", ``, Config{Lifecycle: fakeLifecycle{}}, http.StatusNotFound, "NOT_FOUND", "unknown destination action"},
		{"bad input", http.MethodPost, "/api/v1/flows", `{`, Config{Flows: &fakeFlows{}}, http.StatusBadRequest, "BAD_REQUEST", "not valid JSON"},
		{"not found", http.MethodPut, "/api/v1/flows/f", `{}`, Config{FlowUpdates: fakeUpdater{err: ErrFlowNotFound}}, http.StatusNotFound, "NOT_FOUND", "flow not found"},
		{"conflict", http.MethodPost, "/api/v1/flows/import", `{"version":1,"flows":[{"id":"a"}]}`, Config{Transfer: fakeTransfer{err: ErrImportConflict}}, http.StatusConflict, "CONFLICT", "flows already exist"},
		{"internal error hides detail", http.MethodGet, "/api/v1/flows", ``, Config{Flows: &errFlows{}}, http.StatusInternalServerError, "INTERNAL", "internal error"},
		{"not implemented", http.MethodGet, "/api/v1/flows", ``, Config{Flows: &notImplementedFlows{}}, http.StatusNotImplemented, "NOT_IMPLEMENTED", "not implemented in this edition: flow federation"},
		{"specific code kept", http.MethodPost, "/api/v1/flows/import", `{"version":1,"flows":[{"id":"a"}]}`, Config{Transfer: fakeTransfer{err: ErrImportIncomplete}}, http.StatusInternalServerError, "IMPORT_INCOMPLETE", "stopped part-way"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(tt.cfg).Router().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))
			var env struct {
				Error struct{ Code, Message string } `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("body %q is not the envelope: %v", rec.Body.String(), err)
			}
			if rec.Code != tt.status || env.Error.Code != tt.code || !strings.Contains(env.Error.Message, tt.message) ||
				rec.Header().Get("Content-Type") != "application/json" {
				t.Errorf("got %d %s %q (%s); want %d %s containing %q", rec.Code, env.Error.Code, env.Error.Message,
					rec.Header().Get("Content-Type"), tt.status, tt.code, tt.message)
			}
			if strings.Contains(rec.Body.String(), "store unavailable") {
				t.Errorf("internal detail leaked: %s", rec.Body.String())
			}
		})
	}
}

func TestStatusCodeFallback(t *testing.T) {
	rec := httptest.NewRecorder()
	writeStatusError(rec, http.StatusTeapot, "short and stout")
	if !strings.Contains(rec.Body.String(), `"code":"ERROR"`) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestMethodNotAllowedListsAllowed(t *testing.T) {
	rec := httptest.NewRecorder()
	New(Config{}).Router().ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/api/v1/flows/f", nil))
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, PUT, DELETE" {
		t.Errorf("status %d, Allow %q", rec.Code, rec.Header().Get("Allow"))
	}
}
