package gateway

import (
	"net/http"
	"strings"
	"testing"
)

// TestMetricsRoute: /metrics is mounted only with a handler, needs
// credentials and flows:view, and not the CSRF marker.
func TestMetricsRoute(t *testing.T) {
	metrics := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("weavster_flows 1\n")) })
	s := New(Config{
		Auth: fakeAuth{users: map[string]Identity{
			"viewer": {Username: "viewer", Permissions: []string{"flows:view"}},
			"sender": {Username: "sender", Permissions: []string{"messages:send"}},
		}},
		Authorizer:  fakeAuthz{},
		RequireCSRF: true,
		Metrics:     metrics,
	})
	as := func(user string) func(*http.Request) {
		return func(r *http.Request) {
			r.SetBasicAuth(user, "pw")
			r.Header.Del(MarkerHeader) // Prometheus sends no marker
		}
	}
	for _, tt := range []struct {
		name   string
		server *Server
		set    func(*http.Request)
		status int
	}{
		{"with flows:view", s, as("viewer"), http.StatusOK},
		{"without flows:view", s, as("sender"), http.StatusForbidden},
		{"without credentials", s, func(r *http.Request) { r.Header.Del(MarkerHeader) }, http.StatusUnauthorized},
		{"not mounted", New(Config{}), nil, http.StatusNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if rec := serve(tt.server, http.MethodGet, "/metrics", "", tt.set); rec.Code != tt.status {
				t.Errorf("GET /metrics = %d %s, want %d", rec.Code, rec.Body.String(), tt.status)
			}
		})
	}
}

// TestContextPath: under a context path the API answers with the prefix
// only; other paths, the bare prefix, and an encoded prefix get the
// router's JSON 404 with its security headers.
func TestContextPath(t *testing.T) {
	s := New(Config{ContextPath: "/weavster"})
	for _, tt := range []struct {
		path   string
		status int
	}{
		{"/weavster/api/openapi.yaml", http.StatusOK},
		{"/api/openapi.yaml", http.StatusNotFound},
		{"/weavster", http.StatusNotFound},
		{"/weavsterx/api/openapi.yaml", http.StatusNotFound},
		{"/weav%73ter/api/openapi.yaml", http.StatusOK},
	} {
		rec := serve(s, http.MethodGet, tt.path, "", nil)
		if rec.Code != tt.status || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("GET %s = %d (nosniff %q), want %d", tt.path, rec.Code, rec.Header().Get("X-Content-Type-Options"), tt.status)
		}
		if tt.status == http.StatusNotFound && !strings.Contains(rec.Body.String(), `"code":"NOT_FOUND"`) {
			t.Errorf("GET %s body = %s", tt.path, rec.Body.String())
		}
	}
}
