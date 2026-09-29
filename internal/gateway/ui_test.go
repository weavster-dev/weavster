package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestUIMount: with a UI, / and /ui redirect to /ui/ (under the context
// path) and /ui/... is served without credentials; without one, those paths
// are not found.
func TestUIMount(t *testing.T) {
	ui := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ui " + r.URL.Path)) })
	s := New(Config{UI: ui, Auth: fakeAuth{}})
	for _, tt := range []struct {
		path, location, body string
		status               int
	}{
		{"/", "/ui/", "", http.StatusFound},
		{"/ui", "/ui/", "", http.StatusFound},
		{"/ui/", "", "ui /", http.StatusOK},
		{"/ui/app.js", "", "ui /app.js", http.StatusOK},
	} {
		rec := httptest.NewRecorder()
		s.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
		if rec.Code != tt.status || rec.Header().Get("Location") != tt.location || (tt.body != "" && rec.Body.String() != tt.body) {
			t.Errorf("GET %s = %d %q %q", tt.path, rec.Code, rec.Header().Get("Location"), rec.Body.String())
		}
	}
	for _, path := range []string{"/weavster", "/weavster/", "/weavster/ui"} {
		rec := httptest.NewRecorder()
		New(Config{UI: ui, ContextPath: "/weavster"}).Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/weavster/ui/" {
			t.Errorf("GET %s under /weavster = %d %q", path, rec.Code, rec.Header().Get("Location"))
		}
	}
	head := httptest.NewRecorder()
	s.Router().ServeHTTP(head, httptest.NewRequest(http.MethodHead, "/", nil))
	if head.Code != http.StatusFound || head.Header().Get("Location") != "/ui/" {
		t.Errorf("HEAD / = %d %q", head.Code, head.Header().Get("Location"))
	}
	rec := httptest.NewRecorder()
	New(Config{}).Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("without a UI: %d", rec.Code)
	}
}
