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
		{"not json", http.MethodPost, "/api/v1/config/import", `{`, ports, http.StatusBadRequest, "invalid JSON body"},
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
		{"map needs strings", http.MethodPost, "/api/v1/config/import?overwriteConfigMap=true", `{"format":"weavster-config-v1","configmap":{"a":1}}`, ports, http.StatusBadRequest, "configmap"},
		{"overwrite with an empty map", http.MethodPost, "/api/v1/config/import?overwriteConfigMap=true", `{"format":"weavster-config-v1","configmap":{}}`, ports, http.StatusOK, `"configMapReplaced":true`},
		{"overwrite without a map", http.MethodPost, "/api/v1/config/import?overwriteConfigMap=true", `{"format":"weavster-config-v1"}`, ports, http.StatusBadRequest, "needs a configmap object"},
		{"overwrite with null", http.MethodPost, "/api/v1/config/import?overwriteConfigMap=true", `{"format":"weavster-config-v1","configmap":null}`, ports, http.StatusBadRequest, "needs a configmap object"},
		{"overwrite with an array", http.MethodPost, "/api/v1/config/import?overwriteConfigMap=true", `{"format":"weavster-config-v1","configmap":[]}`, ports, http.StatusBadRequest, "needs a configmap object"},
		{"nodeploy needs no lifecycle", http.MethodPost, "/api/v1/config/import?nodeploy=true", doc, func() Config { c := ports(); c.Lifecycle = nil; return c }, http.StatusOK, `"deployed":[]`},
		{"deploy needs the lifecycle", http.MethodPost, "/api/v1/config/import", doc, func() Config { c := ports(); c.Lifecycle = nil; return c }, http.StatusServiceUnavailable, "unavailable"},
		{"export needs no flows", http.MethodGet, "/api/v1/config/export", ``, func() Config { c := ports(); c.Flows, c.Lifecycle = nil, nil; return c }, http.StatusOK, `"format"`},
		{"map ignored without overwrite", http.MethodPost, "/api/v1/config/import", `{"format":"weavster-config-v1","configmap":{"a":1}}`, ports, http.StatusOK, `"configMapReplaced":false`},
		{"empty map exported", http.MethodGet, "/api/v1/config/export?includeConfigMap=true", ``, ports, http.StatusOK, `"configmap":{}`},
		{"created meanwhile", http.MethodPost, "/api/v1/config/import", `{"format":"weavster-config-v1","alerts":[` + validAlert + `]}`,
			func() Config { c := ports(); c.Alerts = &racingAlerts{memAlerts{alerts: map[string]Alert{}}}; return c }, http.StatusConflict, "created on the server during the import"},
		{"library deleted meanwhile", http.MethodPost, "/api/v1/config/import", `{"format":"weavster-config-v1","snippets":[{"name":"s"}]}`,
			func() Config {
				c := ports()
				c.Snippets = &vanishingLibrary{memSnippets{snippets: map[string]Snippet{}}}
				return c
			}, http.StatusConflict, "was deleted during the import"},
		{"deploy read fails", http.MethodPost, "/api/v1/config/import?force=true", `{"format":"weavster-config-v1","flows":[{"id":"gone"}]}`,
			func() Config { c := ports(); c.Flows = &errFlows{}; return c }, http.StatusInternalServerError, "flow gone could not be read"},
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

// racingAlerts reports every alert as created meanwhile.
type racingAlerts struct{ memAlerts }

func (r *racingAlerts) SaveAlerts(context.Context, []Alert, bool) error { return ErrAlertExists }

// vanishingLibrary loses its libraries while snippets are saved.
type vanishingLibrary struct{ memSnippets }

func (v *vanishingLibrary) SaveSnippets(context.Context, []Snippet, bool) error {
	return ErrLibraryNotFound
}

// depFlows is a flow store whose flow "a" depends on "b": deploying a
// deploys b too.
type depFlows struct{ fakeFlows }

type depLifecycle struct {
	fakeLifecycle
	flows *depFlows
}

func (l depLifecycle) Transition(_ context.Context, id, action string) (Flow, error) {
	for i := range l.flows.flows {
		if f := &l.flows.flows[i]; f.ID == id || (id == "a" && f.ID == "b") {
			f.Status = "deployed"
		}
	}
	return Flow{ID: id}, nil
}

func TestConfigImportReportsDependencies(t *testing.T) {
	flows := &depFlows{fakeFlows{flows: []Flow{{ID: "a", Status: "undeployed", Enabled: true}, {ID: "b", Status: "undeployed"}}}}
	cfg := Config{Transfer: fakeTransfer{}, Flows: flows, Lifecycle: depLifecycle{flows: flows},
		Alerts: &memAlerts{alerts: map[string]Alert{}}, Snippets: &memSnippets{snippets: map[string]Snippet{}, libraries: map[string]SnippetLibrary{}}, Items: memItems{}}
	rec := httptest.NewRecorder()
	New(cfg).Router().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/config/import?force=true",
		strings.NewReader(`{"format":"weavster-config-v1","flows":[{"id":"a"},{"id":"b"}]}`)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"deployed":["a","b"]`) {
		t.Errorf("got %d %s; want a and b deployed", rec.Code, rec.Body.String())
	}
}

// flakyFlows fails reads after the first len(flows)*2 (the deploy pass).
type flakyFlows struct {
	fakeFlows
	reads *int
}

func (f *flakyFlows) Get(ctx context.Context, id string) (Flow, error) {
	*f.reads++
	if *f.reads > 2 {
		return Flow{}, errDisk
	}
	return f.fakeFlows.Get(ctx, id)
}

func TestConfigImportFinalReadFails(t *testing.T) {
	reads := 0
	flows := &flakyFlows{fakeFlows{flows: []Flow{{ID: "a", Status: "undeployed", Enabled: true}}}, &reads}
	cfg := Config{Transfer: fakeTransfer{}, Flows: flows, Lifecycle: fakeLifecycle{},
		Alerts: &memAlerts{alerts: map[string]Alert{}}, Snippets: &memSnippets{snippets: map[string]Snippet{}, libraries: map[string]SnippetLibrary{}}, Items: memItems{}}
	rec := httptest.NewRecorder()
	New(cfg).Router().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/config/import?force=true",
		strings.NewReader(`{"format":"weavster-config-v1","flows":[{"id":"a"}]}`)))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "could not be read after deploying") {
		t.Errorf("got %d %s", rec.Code, rec.Body.String())
	}
}

func TestQueryTrue(t *testing.T) {
	for query, want := range map[string]bool{"": false, "x=false": false, "x=0": false, "x=true": true, "x=1": true, "x=maybe": true} {
		if got := queryTrue("x")(httptest.NewRequest(http.MethodGet, "/?"+query, nil)); got != want {
			t.Errorf("%q: %v, want %v", query, got, want)
		}
	}
}

// failingLibraries fails every library lookup.
type failingLibraries struct{ memSnippets }

func (f *failingLibraries) GetLibrary(_ context.Context, _ string) (SnippetLibrary, error) {
	return SnippetLibrary{}, errDisk
}
