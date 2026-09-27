package gateway

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfigTransferHandlers(t *testing.T) {
	ports := func() Config {
		return Config{
			Transfer: fakeTransfer{}, Flows: &fakeFlows{flows: []Flow{{ID: "a", Status: "undeployed", Enabled: true}}},
			Lifecycle: fakeLifecycle{}, Alerts: &memAlerts{alerts: map[string]Alert{}},
			Snippets: &memSnippets{snippets: map[string]Snippet{}, libraries: map[string]SnippetLibrary{}}, Items: memItems{},
		}
	}
	doc := `{"format":"weavster-config-v1","flows":[{"id":"n"}],"scripts":{"s":"x"},"settings":{"k":1}}`
	tests := []struct {
		name, method, path, body string
		cfg                      func() Config
		status                   int
		want                     string
	}{
		{"export unavailable", http.MethodGet, "/api/v1/config/export", ``, func() Config { return Config{} }, http.StatusServiceUnavailable, "unavailable"},
		{"import unavailable", http.MethodPost, "/api/v1/config/import", doc, func() Config { return Config{} }, http.StatusServiceUnavailable, "unavailable"},
		{"export", http.MethodGet, "/api/v1/config/export", ``, ports, http.StatusOK, `"flows":[{"id":"a"`},
		{"export bad flag", http.MethodGet, "/api/v1/config/export?includeConfigMap=yes", ``, ports, http.StatusBadRequest, "includeConfigMap must be"},
		{"export fails", http.MethodGet, "/api/v1/config/export", ``, func() Config { c := ports(); c.Transfer = fakeTransfer{err: errDisk}; return c }, http.StatusInternalServerError, "internal error"},
		{"import", http.MethodPost, "/api/v1/config/import", doc, ports, http.StatusOK, `"scripts":1,"settings":1`},
		{"import deploys", http.MethodPost, "/api/v1/config/import", doc, ports, http.StatusOK, `"deployed":[]`},
		{"bad flag", http.MethodPost, "/api/v1/config/import?force=x", doc, ports, http.StatusBadRequest, "force must be"},
		{"not json", http.MethodPost, "/api/v1/config/import", `{`, ports, http.StatusBadRequest, "invalid configuration document"},
		{"trailing", http.MethodPost, "/api/v1/config/import", doc + `{}`, ports, http.StatusBadRequest, "trailing data"},
		{"unknown field", http.MethodPost, "/api/v1/config/import", `{"format":"weavster-config-v1","users":[]}`, ports, http.StatusBadRequest, "unknown field"},
		{"too large", http.MethodPost, "/api/v1/config/import", `{"format":"` + strings.Repeat("x", maxImportBytes) + `"}`, ports, http.StatusRequestEntityTooLarge, "50 MiB"},
		{"bad flow", http.MethodPost, "/api/v1/config/import", `{"format":"weavster-config-v1","flows":[{"id":"a b"}]}`, ports, http.StatusBadRequest, "flows[0]"},
		{"bad library", http.MethodPost, "/api/v1/config/import", `{"format":"weavster-config-v1","snippetLibraries":[{"name":""}]}`, ports, http.StatusBadRequest, "library name"},
		{"bad snippet", http.MethodPost, "/api/v1/config/import", `{"format":"weavster-config-v1","snippets":[{"name":"a","library":"a b"}]}`, ports, http.StatusBadRequest, "library: name"},
		{"repeated snippet", http.MethodPost, "/api/v1/config/import", `{"format":"weavster-config-v1","snippets":[{"name":"a"},{"name":"a"}]}`, ports, http.StatusBadRequest, "more than once"},
		{"bad alert id", http.MethodPost, "/api/v1/config/import", `{"format":"weavster-config-v1","alerts":[{"id":""}]}`, ports, http.StatusBadRequest, "alert id"},
		{"bad alert", http.MethodPost, "/api/v1/config/import", `{"format":"weavster-config-v1","alerts":[{"id":"x","name":"X"}]}`, ports, http.StatusBadRequest, "trigger.events"},
		{"bad setting name", http.MethodPost, "/api/v1/config/import", `{"format":"weavster-config-v1","settings":{"a b":1}}`, ports, http.StatusBadRequest, "settings: name"},
		{"map needs strings", http.MethodPost, "/api/v1/config/import", `{"format":"weavster-config-v1","configmap":{"a":1}}`, ports, http.StatusBadRequest, "configmap"},
		{"library lookup fails", http.MethodPost, "/api/v1/config/import", `{"format":"weavster-config-v1","snippets":[{"name":"s","library":"l"}]}`,
			func() Config { c := ports(); c.Snippets = &failingLibraries{memSnippets{fail: true}}; return c }, http.StatusInternalServerError, "internal error"},
		{"conflict lookup fails", http.MethodPost, "/api/v1/config/import", doc, func() Config { c := ports(); c.Flows = &errFlows{}; return c }, http.StatusInternalServerError, "internal error"},
		{"conflict", http.MethodPost, "/api/v1/config/import", `{"format":"weavster-config-v1","flows":[{"id":"a"}]}`, ports, http.StatusConflict, "flow a"},
		{"flow import conflict", http.MethodPost, "/api/v1/config/import?force=true", doc,
			func() Config {
				c := ports()
				c.Transfer = fakeTransfer{err: fmt.Errorf("%w: a", ErrImportConflict)}
				return c
			}, http.StatusConflict, "a"},
		{"write fails", http.MethodPost, "/api/v1/config/import?force=true", `{"format":"weavster-config-v1","alerts":[` + validAlert + `]}`,
			func() Config { c := ports(); c.Alerts = &memAlerts{fail: true}; return c }, http.StatusInternalServerError, "part-way"},
		{"deploy fails", http.MethodPost, "/api/v1/config/import?force=true", `{"format":"weavster-config-v1","flows":[{"id":"a"}]}`,
			func() Config { c := ports(); c.Lifecycle = fakeLifecycle{err: errDisk}; return c }, http.StatusInternalServerError, "flow a did not deploy"},
		{"overwrite map", http.MethodPost, "/api/v1/config/import?overwriteConfigMap=true&nodeploy=true", `{"format":"weavster-config-v1","configmap":{"a":"b"}}`, ports, http.StatusOK, `"configMapReplaced":true`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(tt.cfg()).Router().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("got %d %.300q; want %d containing %q", rec.Code, rec.Body.String(), tt.status, tt.want)
			}
		})
	}
}

// failingLibraries fails every library lookup.
type failingLibraries struct{ memSnippets }

func (f *failingLibraries) GetLibrary(_ context.Context, _ string) (SnippetLibrary, error) {
	return SnippetLibrary{}, errDisk
}
