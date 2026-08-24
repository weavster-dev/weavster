// Package e2e holds end-to-end tests that exercise the Weavster HTTP API over a
// real network listener — the full middleware + routing + handler stack, not
// package-internal unit tests.
package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/observability"
)

// newServer starts a real gateway HTTP server. requireCSRF toggles the CSRF
// marker middleware so the marker behaviour can be exercised end-to-end.
func newServer(t *testing.T, requireCSRF bool) *httptest.Server {
	t.Helper()
	gw := gateway.New(gateway.Config{
		System:      observability.SystemStatus("e2e-instance", "0.0.0-test", "now"),
		RequireCSRF: requireCSRF,
	})
	ts := httptest.NewServer(gw.Router())
	t.Cleanup(ts.Close)
	return ts
}

func get(t *testing.T, url string, header http.Header) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, body
}

func TestOpenAPISpec(t *testing.T) {
	ts := newServer(t, false)
	resp, body := get(t, ts.URL+"/api/openapi.yaml", nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/yaml" {
		t.Errorf("Content-Type = %q, want application/yaml", ct)
	}
	if !strings.Contains(string(body), "openapi: 3.1.0") {
		t.Errorf("body missing openapi version marker")
	}
}

func TestSystemEndpoint(t *testing.T) {
	ts := newServer(t, false)
	resp, body := get(t, ts.URL+"/api/v1/system", nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var info observability.SystemInfo
	if err := json.Unmarshal(body, &info); err != nil {
		t.Fatalf("decode system JSON: %v", err)
	}
	if info.ID != "e2e-instance" {
		t.Errorf("system id = %q, want e2e-instance", info.ID)
	}
}

func TestSecurityHeaders(t *testing.T) {
	ts := newServer(t, false)
	resp, _ := get(t, ts.URL+"/api/v1/system", nil)

	want := map[string]string{
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
		"X-Frame-Options":           "DENY",
		"X-Content-Type-Options":    "nosniff",
	}
	for header, val := range want {
		if got := resp.Header.Get(header); got != val {
			t.Errorf("%s = %q, want %q", header, got, val)
		}
	}
}

func TestBlockTraceAndTrack(t *testing.T) {
	ts := newServer(t, false)

	for _, method := range []string{http.MethodTrace, "TRACK"} {
		req, err := http.NewRequest(method, ts.URL+"/api/v1/system", nil)
		if err != nil {
			t.Fatalf("build %s request: %v", method, err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s status = %d, want %d", method, resp.StatusCode, http.StatusMethodNotAllowed)
		}
	}
}

func TestCSRFMarkerRequired(t *testing.T) {
	ts := newServer(t, true)

	// Without the marker, the API rejects the request.
	resp, _ := get(t, ts.URL+"/api/v1/system", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("no-marker status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	// With the marker, the request succeeds.
	resp, _ = get(t, ts.URL+"/api/v1/system", http.Header{gateway.MarkerHeader: {gateway.MarkerValue}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("with-marker status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}
