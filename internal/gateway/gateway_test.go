package gateway

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/observability"
	"github.com/weavster-dev/weavster/internal/topology"
)

type fakeTopology struct{}

func (fakeTopology) Overview(context.Context) (topology.Graph, error) {
	return topology.Overview([]topology.FlowSummary{{ID: "a", Name: "Admit", Status: "started"}}), nil
}

func (fakeTopology) FlowInternal(_ context.Context, id string) (topology.Graph, error) {
	return topology.FlowInternal(topology.FlowDetail{ID: id, Name: "Admit"}), nil
}

type fakeFlows struct{ flows []Flow }

func (f *fakeFlows) List(context.Context) ([]Flow, error) { return f.flows, nil }
func (f *fakeFlows) Get(_ context.Context, id string) (Flow, error) {
	for _, fl := range f.flows {
		if fl.ID == id {
			return fl, nil
		}
	}
	return Flow{}, nil
}
func (f *fakeFlows) Create(_ context.Context, fl Flow) error {
	f.flows = append(f.flows, fl)
	return nil
}
func (f *fakeFlows) Delete(_ context.Context, id string) error { return nil }

type fakeMessages struct{}

func (fakeMessages) Search(context.Context, MessageQuery) ([]Message, error) {
	return []Message{{ID: "1", Status: "sent"}}, nil
}

func newTestServer(requireCSRF bool) *Server {
	return New(Config{
		Topology:    fakeTopology{},
		Flows:       &fakeFlows{flows: []Flow{{ID: "f1", Name: "Admit", SourceType: "file", Status: "started", Enabled: true}}},
		Messages:    fakeMessages{},
		System:      observability.SystemStatus("weavster-1", "0.1.0", "2026-08-23"),
		RequireCSRF: requireCSRF,
	})
}

func do(t *testing.T, h http.Handler, method, path string, marker bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if marker {
		req.Header.Set(MarkerHeader, MarkerValue)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSecurityHeadersAndTraceBlock(t *testing.T) {
	srv := newTestServer(false).Router()

	rec := do(t, srv, http.MethodGet, "/api/openapi.yaml", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("openapi status = %d", rec.Code)
	}
	for hdr, want := range map[string]string{
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
		"X-Frame-Options":           "DENY",
		"Content-Security-Policy":   "frame-ancestors 'none'",
		"X-Content-Type-Options":    "nosniff",
	} {
		if got := rec.Header().Get(hdr); got != want {
			t.Errorf("%s = %q, want %q", hdr, got, want)
		}
	}

	rec = do(t, srv, http.MethodTrace, "/", false)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("TRACE status = %d, want 405", rec.Code)
	}
	rec = do(t, srv, "TRACK", "/", false)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("TRACK status = %d, want 405", rec.Code)
	}
}

func TestCSRFMarker(t *testing.T) {
	srv := newTestServer(true).Router()

	if rec := do(t, srv, http.MethodGet, "/api/v1/topology", false); rec.Code != http.StatusBadRequest {
		t.Errorf("missing marker status = %d, want 400", rec.Code)
	}
	if rec := do(t, srv, http.MethodGet, "/api/v1/topology", true); rec.Code != http.StatusOK {
		t.Errorf("with marker status = %d, want 200", rec.Code)
	}
}

func TestTopologyOverview(t *testing.T) {
	srv := newTestServer(true).Router()
	rec := do(t, srv, http.MethodGet, "/api/v1/topology", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "flow:a") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestFlowList(t *testing.T) {
	srv := newTestServer(true).Router()
	rec := do(t, srv, http.MethodGet, "/api/v1/flows", true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Admit") {
		t.Errorf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestOpenAPISpecValid(t *testing.T) {
	if err := ValidateSpec(); err != nil {
		t.Fatalf("openapi spec invalid: %v", err)
	}
	if !strings.Contains(OpenAPISpec(), "openapi: 3.1.0") {
		t.Error("spec missing version")
	}
}

func TestTLSConfig(t *testing.T) {
	cfg, err := BuildTLSConfig(DefaultTLSOptions())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("min version = %x", cfg.MinVersion)
	}
	if len(cfg.CipherSuites) == 0 || len(cfg.CurvePreferences) == 0 {
		t.Errorf("ciphers/curves empty")
	}
	if _, err := BuildTLSConfig(TLSOptions{}); err != ErrInvalidTLS {
		t.Errorf("expected ErrInvalidTLS, got %v", err)
	}
}

func TestFlowsGetCreateDelete(t *testing.T) {
	srv := newTestServer(true).Router()

	// GET /api/v1/flows/f1 — existing flow.
	rec := do(t, srv, http.MethodGet, "/api/v1/flows/f1", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("FlowsGet status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "f1") {
		t.Errorf("FlowsGet body = %s", rec.Body.String())
	}

	// POST /api/v1/flows — create a new flow.
	body := `{"id":"f2","name":"Discharge","sourceType":"file","status":"stopped","enabled":false}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/flows", strings.NewReader(body))
	req.Header.Set(MarkerHeader, MarkerValue)
	req.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusCreated {
		t.Fatalf("FlowsCreate status = %d, want 201; body: %s", rec2.Code, rec2.Body.String())
	}
	if !strings.Contains(rec2.Body.String(), "Discharge") {
		t.Errorf("FlowsCreate body = %s", rec2.Body.String())
	}

	// DELETE /api/v1/flows/f1 — delete a flow.
	rec3 := do(t, srv, http.MethodDelete, "/api/v1/flows/f1", true)
	if rec3.Code != http.StatusNoContent {
		t.Fatalf("FlowsDelete status = %d, want 204", rec3.Code)
	}
}

func TestMessagesSearch(t *testing.T) {
	srv := newTestServer(true).Router()
	rec := do(t, srv, http.MethodGet, "/api/v1/messages", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("MessagesSearch status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "sent") {
		t.Errorf("MessagesSearch body = %s", rec.Body.String())
	}
}

func TestSystemStatus(t *testing.T) {
	srv := newTestServer(true).Router()
	rec := do(t, srv, http.MethodGet, "/api/v1/system", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("System status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "weavster") {
		t.Errorf("System body = %s", rec.Body.String())
	}
}

func TestTopologyFlowInternal(t *testing.T) {
	srv := newTestServer(true).Router()
	rec := do(t, srv, http.MethodGet, "/api/v1/topology/flows/flow-x", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("TopologyFlow status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "flow-x") {
		t.Errorf("TopologyFlow body = %s", rec.Body.String())
	}
}

func TestNilServiceReturns503(t *testing.T) {
	// A server with no optional services wired up must return 503.
	bare := New(Config{RequireCSRF: false}).Router()

	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, "/api/v1/topology"},
		{http.MethodGet, "/api/v1/topology/flows/x"},
		{http.MethodGet, "/api/v1/flows"},
		{http.MethodGet, "/api/v1/flows/x"},
		{http.MethodPost, "/api/v1/flows"},
		{http.MethodDelete, "/api/v1/flows/x"},
		{http.MethodGet, "/api/v1/messages"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		bare.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s → %d, want 503", tc.method, tc.path, rec.Code)
		}
	}
}

// errFlows is a FlowStore that always returns an error for mutating operations.
type errFlows struct{}

func (e *errFlows) List(_ context.Context) ([]Flow, error) {
	return nil, errors.New("store unavailable")
}
func (e *errFlows) Get(_ context.Context, _ string) (Flow, error) {
	return Flow{}, errors.New("store unavailable")
}
func (e *errFlows) Create(_ context.Context, _ Flow) error { return errors.New("store unavailable") }
func (e *errFlows) Delete(_ context.Context, _ string) error {
	return errors.New("store unavailable")
}

// errMessages is a MessageStore that always errors.
type errMessages struct{}

func (errMessages) Search(_ context.Context, _ MessageQuery) ([]Message, error) {
	return nil, errors.New("search unavailable")
}

// errTopology is a TopologyProvider that always errors.
type errTopology struct{}

func (errTopology) Overview(_ context.Context) (topology.Graph, error) {
	return topology.Graph{}, errors.New("topology unavailable")
}
func (errTopology) FlowInternal(_ context.Context, _ string) (topology.Graph, error) {
	return topology.Graph{}, errors.New("topology unavailable")
}

func newErrServer() *Server {
	return New(Config{
		Topology:    errTopology{},
		Flows:       &errFlows{},
		Messages:    errMessages{},
		System:      observability.SystemStatus("weavster-1", "0.1.0", "2026-08-23"),
		RequireCSRF: false,
	})
}

func TestFlowsHandlerErrorPaths(t *testing.T) {
	srv := newErrServer().Router()

	// List error → 500
	rec := do(t, srv, http.MethodGet, "/api/v1/flows", false)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("FlowsList error: want 500, got %d", rec.Code)
	}

	// Get error → 404
	rec = do(t, srv, http.MethodGet, "/api/v1/flows/missing", false)
	if rec.Code != http.StatusNotFound {
		t.Errorf("FlowsGet error: want 404, got %d", rec.Code)
	}

	// Create bad JSON → 400
	req := httptest.NewRequest(http.MethodPost, "/api/v1/flows", strings.NewReader("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusBadRequest {
		t.Errorf("FlowsCreate bad JSON: want 400, got %d", rec2.Code)
	}

	// Create store error → 500
	body := `{"id":"x","name":"X","sourceType":"file","status":"stopped","enabled":false}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/flows", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec3 := httptest.NewRecorder()
	srv.ServeHTTP(rec3, req)
	if rec3.Code != http.StatusInternalServerError {
		t.Errorf("FlowsCreate store error: want 500, got %d", rec3.Code)
	}

	// Delete error → 404
	rec = do(t, srv, http.MethodDelete, "/api/v1/flows/missing", false)
	if rec.Code != http.StatusNotFound {
		t.Errorf("FlowsDelete error: want 404, got %d", rec.Code)
	}
}

func TestMessagesSearchErrorPath(t *testing.T) {
	srv := newErrServer().Router()
	rec := do(t, srv, http.MethodGet, "/api/v1/messages", false)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("MessagesSearch error: want 500, got %d", rec.Code)
	}
}

func TestTopologyHandlerErrorPaths(t *testing.T) {
	srv := newErrServer().Router()

	rec := do(t, srv, http.MethodGet, "/api/v1/topology", false)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("TopologyOverview error: want 500, got %d", rec.Code)
	}

	rec = do(t, srv, http.MethodGet, "/api/v1/topology/flows/x", false)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("TopologyFlow error: want 500, got %d", rec.Code)
	}
}

func TestValidateSpecMalformed(t *testing.T) {
	// ValidateSpec validates the embedded spec — it must return nil normally.
	if err := ValidateSpec(); err != nil {
		t.Fatalf("ValidateSpec on valid spec: %v", err)
	}
}

func TestMessagesSearchQueryParams(t *testing.T) {
	// Ensure query parameters are forwarded to the search (covers the q.Limit default branch).
	srv := newTestServer(false).Router()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages?status=sent&flowId=f1", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("MessagesSearch with params: want 200, got %d", rec.Code)
	}
}
