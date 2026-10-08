package gateway

import (
	"context"
	"encoding/json"
	"errors"
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

// notImplementedSearch answers message and event searches with the D-17
// sentinel.
type notImplementedSearch struct{ messageOps }

func (notImplementedSearch) Search(context.Context, MessageQuery) ([]Message, error) {
	return nil, fmt.Errorf("%w: message archive", enterprise.ErrNotImplemented)
}

func (notImplementedSearch) SearchEvents(context.Context, EventQuery) ([]Event, error) {
	return nil, fmt.Errorf("%w: event archive", enterprise.ErrNotImplemented)
}
func (notImplementedSearch) GetEvent(context.Context, int64) (Event, error) {
	return Event{}, fmt.Errorf("%w: event archive", enterprise.ErrNotImplemented)
}
func (notImplementedSearch) CountEvents(context.Context, EventQuery) (int, error) {
	return 0, fmt.Errorf("%w: event archive", enterprise.ErrNotImplemented)
}
func (notImplementedSearch) MaxEventID(context.Context) (int64, error) {
	return 0, fmt.Errorf("%w: event archive", enterprise.ErrNotImplemented)
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
		{"not implemented messages", http.MethodGet, "/api/v1/messages", ``, Config{Messages: notImplementedSearch{}}, http.StatusNotImplemented, "NOT_IMPLEMENTED", "message archive"},
		{"not implemented events", http.MethodGet, "/api/v1/events", ``, Config{Events: notImplementedSearch{}}, http.StatusNotImplemented, "NOT_IMPLEMENTED", "event archive"},
		{"not implemented event", http.MethodGet, "/api/v1/events/1", ``, Config{Events: notImplementedSearch{}}, http.StatusNotImplemented, "NOT_IMPLEMENTED", "event archive"},
		{"not implemented event count", http.MethodGet, "/api/v1/events/count", ``, Config{Events: notImplementedSearch{}}, http.StatusNotImplemented, "NOT_IMPLEMENTED", "event archive"},
		{"not implemented max id", http.MethodGet, "/api/v1/events/max-id", ``, Config{Events: notImplementedSearch{}}, http.StatusNotImplemented, "NOT_IMPLEMENTED", "event archive"},
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
	for _, method := range []string{http.MethodPatch, http.MethodTrace, "TRACK"} {
		rec := httptest.NewRecorder()
		New(Config{}).Router().ServeHTTP(rec, httptest.NewRequest(method, "/api/v1/flows/f", nil))
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, PUT, DELETE" {
			t.Errorf("%s: status %d, Allow %q", method, rec.Code, rec.Header().Get("Allow"))
		}
	}
}

func TestUnversionedPaths(t *testing.T) {
	tests := []struct {
		method, path string
		status       int
		version      string
	}{
		{http.MethodGet, "/api/v1/flows", http.StatusOK, "v1"},
		{http.MethodGet, "/api/flows", http.StatusOK, "v1"},
		{http.MethodGet, "/api/flows/f/stats", http.StatusServiceUnavailable, "v1"},
		{http.MethodGet, "/api/v2/flows", http.StatusNotFound, ""},
		{http.MethodGet, "/api/openapi.yaml", http.StatusOK, ""},
		{http.MethodPatch, "/api/flows/f", http.StatusMethodNotAllowed, "v1"},
		{http.MethodGet, "/api/", http.StatusNotFound, ""},
		{http.MethodGet, "/api/v1/nope", http.StatusNotFound, "v1"},
		// An escaped slash stays part of the id on both paths.
		{http.MethodGet, "/api/v1/flows/f%2Fstats", http.StatusOK, "v1"},
		{http.MethodGet, "/api/flows/f%2Fstats", http.StatusOK, "v1"},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		New(Config{Flows: &fakeFlows{}}).Router().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
		if rec.Code != tt.status || rec.Header().Get("Weavster-API-Version") != tt.version {
			t.Errorf("%s %s: %d, version %q; want %d, %q", tt.method, tt.path, rec.Code, rec.Header().Get("Weavster-API-Version"), tt.status, tt.version)
		}
	}
}

func TestIsVersion(t *testing.T) {
	for seg, want := range map[string]bool{"v1": true, "v12": true, "v": false, "vx": false, "V1": false, "flows": false, "": false} {
		if got := isVersion(seg); got != want {
			t.Errorf("isVersion(%q) = %v, want %v", seg, got, want)
		}
	}
}

func TestUserHandlersUnavailableAndBadInput(t *testing.T) {
	tests := []struct {
		method, path, body string
		status             int
	}{
		{http.MethodGet, "/api/v1/users", ``, http.StatusServiceUnavailable},
		{http.MethodGet, "/api/v1/users/x", ``, http.StatusServiceUnavailable},
		{http.MethodPost, "/api/v1/users", `{}`, http.StatusServiceUnavailable},
		{http.MethodPut, "/api/v1/users/x", `{}`, http.StatusServiceUnavailable},
		{http.MethodDelete, "/api/v1/users/x", ``, http.StatusServiceUnavailable},
		{http.MethodPost, "/api/v1/users/x/password", `{}`, http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		New(Config{}).Router().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))
		if rec.Code != tt.status {
			t.Errorf("%s %s: %d, want %d", tt.method, tt.path, rec.Code, tt.status)
		}
	}
}

// TestBackendErrorStatus: a busy server answers 503 with Retry-After; a
// request whose client left 503; other errors 500 without detail.
func TestBackendErrorStatus(t *testing.T) {
	for _, tt := range []struct {
		err        error
		status     int
		retryAfter string
		message    string
	}{
		{fmt.Errorf("ingest: %w", ErrBusy), http.StatusServiceUnavailable, "1", "the server is busy"},
		{fmt.Errorf("acquire: %w", context.Canceled), http.StatusServiceUnavailable, "", "request cancelled"},
		{errors.New("disk: /secret/path"), http.StatusInternalServerError, "", "internal error"},
	} {
		rec := httptest.NewRecorder()
		writeBackendError(rec, tt.err)
		if rec.Code != tt.status || rec.Header().Get("Retry-After") != tt.retryAfter || !strings.Contains(rec.Body.String(), tt.message) {
			t.Errorf("%v: %d %v %s", tt.err, rec.Code, rec.Header(), rec.Body)
		}
	}
}
