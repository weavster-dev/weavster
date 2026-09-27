package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// memAlerts is an in-memory AlertStore; fail makes list and save fail.
type memAlerts struct {
	alerts map[string]Alert
	fail   bool
}

func (m *memAlerts) ListAlerts(context.Context) ([]Alert, error) {
	if m.fail {
		return nil, errDisk
	}
	out := []Alert{}
	for _, a := range m.alerts {
		out = append(out, a)
	}
	return out, nil
}

func (m *memAlerts) GetAlert(_ context.Context, id string) (Alert, error) {
	a, ok := m.alerts[id]
	if !ok {
		return a, ErrAlertNotFound
	}
	return a, nil
}

func (m *memAlerts) SaveAlerts(_ context.Context, list []Alert, create bool) error {
	if m.fail {
		return errDisk
	}
	for _, a := range list {
		if _, ok := m.alerts[a.ID]; ok && create {
			return ErrAlertExists
		}
	}
	for _, a := range list {
		m.alerts[a.ID] = a
	}
	return nil
}

func (m *memAlerts) DeleteAlert(_ context.Context, id string) error {
	if _, ok := m.alerts[id]; !ok {
		return ErrAlertNotFound
	}
	delete(m.alerts, id)
	return nil
}

func (m *memAlerts) SetAlertEnabled(_ context.Context, id string, enabled bool) (Alert, error) {
	a, ok := m.alerts[id]
	if !ok {
		return a, ErrAlertNotFound
	}
	a.Enabled = enabled
	m.alerts[id] = a
	return a, nil
}

const validAlert = `{"id":"errors","name":"Errors","enabled":true,"trigger":{"events":["message.errored"],"flows":["adt"]},"actions":[{"type":"email","to":["ops@example.com"]},{"type":"webhook","url":"https://hooks.example.com/x"}]}`

func TestCheckAlert(t *testing.T) {
	ok := Alert{ID: "a", Name: "A", Trigger: AlertTrigger{Events: []string{"message.queued"}}, Actions: []AlertAction{{Type: "webhook", URL: "http://h/x"}}}
	tests := []struct {
		name   string
		change func(*Alert)
		want   string
	}{
		{"valid", func(*Alert) {}, ""},
		{"reserved id", func(a *Alert) { a.ID = "import" }, "reserved"},
		{"no name", func(a *Alert) { a.Name = "" }, "name must be"},
		{"no events", func(a *Alert) { a.Trigger.Events = nil }, "at least one of"},
		{"unknown event", func(a *Alert) { a.Trigger.Events = []string{"message.sent"} }, "unknown trigger event"},
		{"bad flow", func(a *Alert) { a.Trigger.Flows = []string{"a b"} }, "trigger.flows"},
		{"no actions", func(a *Alert) { a.Actions = nil }, "at least one action"},
		{"email without to", func(a *Alert) { a.Actions = []AlertAction{{Type: "email"}} }, "needs to"},
		{"email with url", func(a *Alert) { a.Actions = []AlertAction{{Type: "email", To: []string{"a@b.c"}, URL: "http://h"}} }, "needs to"},
		{"bad address", func(a *Alert) { a.Actions = []AlertAction{{Type: "email", To: []string{"nobody"}}} }, "not an email address"},
		{"webhook without url", func(a *Alert) { a.Actions = []AlertAction{{Type: "webhook"}} }, "needs url"},
		{"webhook ftp", func(a *Alert) { a.Actions = []AlertAction{{Type: "webhook", URL: "ftp://h/x"}} }, "needs url"},
		{"webhook with to", func(a *Alert) { a.Actions = []AlertAction{{Type: "webhook", URL: "http://h", To: []string{"a@b.c"}}} }, "needs url"},
		{"unknown type", func(a *Alert) { a.Actions = []AlertAction{{Type: "sms"}} }, "type must be one of"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := ok
			tt.change(&a)
			err := checkAlert(a)
			if (tt.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tt.want)) {
				t.Errorf("checkAlert = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestAlertHandlers(t *testing.T) {
	cfg := Config{Alerts: &memAlerts{alerts: map[string]Alert{}}}
	other := strings.Replace(validAlert, `"id":"errors"`, `"id":"dead"`, 1)
	tests := []struct {
		name, method, path, body string
		status                   int
		want                     string
	}{
		{"create", http.MethodPost, "/api/v1/alerts", validAlert, http.StatusCreated, `"id":"errors"`},
		{"create exists", http.MethodPost, "/api/v1/alerts", validAlert, http.StatusConflict, "already exists"},
		{"create invalid", http.MethodPost, "/api/v1/alerts", `{"id":"x","name":"X","trigger":{"events":[]},"actions":[]}`, http.StatusBadRequest, "trigger.events"},
		{"get", http.MethodGet, "/api/v1/alerts/errors", ``, http.StatusOK, `"flows":["adt"]`},
		{"get missing", http.MethodGet, "/api/v1/alerts/zz", ``, http.StatusNotFound, "alert not found"},
		{"get bad id", http.MethodGet, "/api/v1/alerts/a%20b", ``, http.StatusBadRequest, "must be 1-128"},
		{"put", http.MethodPut, "/api/v1/alerts/errors", strings.Replace(validAlert, `"name":"Errors"`, `"name":"All errors"`, 1), http.StatusOK, `"name":"All errors"`},
		{"put id mismatch", http.MethodPut, "/api/v1/alerts/other", validAlert, http.StatusBadRequest, "does not match"},
		{"disable", http.MethodPost, "/api/v1/alerts/errors/disable", ``, http.StatusOK, `"enabled":false`},
		{"enable", http.MethodPost, "/api/v1/alerts/errors/enable", ``, http.StatusOK, `"enabled":true`},
		{"enable missing", http.MethodPost, "/api/v1/alerts/zz/enable", ``, http.StatusNotFound, "alert not found"},
		{"enable bad id", http.MethodPost, "/api/v1/alerts/a%20b/enable", ``, http.StatusBadRequest, "must be 1-128"},
		{"import conflict", http.MethodPost, "/api/v1/alerts/import", `[` + validAlert + `,` + other + `]`, http.StatusConflict, "already exists"},
		{"import force", http.MethodPost, "/api/v1/alerts/import?force=true", `[` + validAlert + `,` + other + `]`, http.StatusOK, `"id":"dead"`},
		{"import bad force", http.MethodPost, "/api/v1/alerts/import?force=maybe", `[]`, http.StatusBadRequest, "force must be"},
		{"import not array", http.MethodPost, "/api/v1/alerts/import", validAlert, http.StatusBadRequest, "invalid JSON body"},
		{"list", http.MethodGet, "/api/v1/alerts", ``, http.StatusOK, `[{"id":"dead"`},
		{"options", http.MethodGet, "/api/v1/alerts/options", ``, http.StatusOK, `"actionTypes":["email","webhook"]`},
		{"delete", http.MethodDelete, "/api/v1/alerts/dead", ``, http.StatusNoContent, ""},
		{"delete missing", http.MethodDelete, "/api/v1/alerts/dead", ``, http.StatusNotFound, "alert not found"},
		{"delete bad id", http.MethodDelete, "/api/v1/alerts/a%20b", ``, http.StatusBadRequest, "must be 1-128"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(cfg).Router().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("got %d %q; want %d containing %q", rec.Code, rec.Body.String(), tt.status, tt.want)
			}
		})
	}
	failing := Config{Alerts: &memAlerts{fail: true}}
	for _, tt := range []struct {
		cfg          Config
		method, path string
		status       int
	}{
		{Config{}, http.MethodGet, "/api/v1/alerts", http.StatusServiceUnavailable},
		{Config{}, http.MethodPost, "/api/v1/alerts", http.StatusServiceUnavailable},
		{Config{}, http.MethodPost, "/api/v1/alerts/import", http.StatusServiceUnavailable},
		{Config{}, http.MethodGet, "/api/v1/alerts/a", http.StatusServiceUnavailable},
		{Config{}, http.MethodDelete, "/api/v1/alerts/a", http.StatusServiceUnavailable},
		{Config{}, http.MethodPost, "/api/v1/alerts/a/enable", http.StatusServiceUnavailable},
		{failing, http.MethodGet, "/api/v1/alerts", http.StatusInternalServerError},
		{failing, http.MethodPost, "/api/v1/alerts", http.StatusInternalServerError},
	} {
		rec := httptest.NewRecorder()
		New(tt.cfg).Router().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, strings.NewReader(validAlert)))
		if rec.Code != tt.status {
			t.Errorf("%s %s: %d %s; want %d", tt.method, tt.path, rec.Code, rec.Body.String(), tt.status)
		}
	}
}
