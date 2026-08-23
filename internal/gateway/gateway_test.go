package gateway

import (
	"context"
	"crypto/tls"
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
