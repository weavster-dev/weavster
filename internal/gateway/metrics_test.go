package gateway

import (
	"net/http"
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
