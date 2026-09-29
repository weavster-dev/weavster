package gateway

import (
	"net/http"
	"strings"
	"testing"
)

func TestAlertInfo(t *testing.T) {
	alerts := &memAlerts{alerts: map[string]Alert{
		"errors": {ID: "errors", Name: "Errors", Enabled: true,
			Trigger: AlertTrigger{Events: []string{"message.errored"}, Flows: []string{"adt"}},
			Actions: []AlertAction{{Type: "email", To: []string{"ops@example.com"}}}},
		"any": {ID: "any", Name: "Any flow", Trigger: AlertTrigger{Events: []string{"message.queued"}},
			Actions: []AlertAction{{Type: "webhook", URL: "https://hooks.example.com/x"}}},
	}}
	s := New(Config{Alerts: alerts})
	for _, tt := range []struct {
		name, method, path, body string
		status                   int
		want                     string
	}{
		{"statuses", http.MethodGet, "/api/v1/alerts/statuses", "", http.StatusOK,
			`[{"id":"any","name":"Any flow","enabled":false},{"id":"errors","name":"Errors","enabled":true}]`},
		{"info", http.MethodGet, "/api/v1/alerts/errors/info", "", http.StatusOK, `"alert":{"id":"errors"`},
		{"info lists the options", http.MethodGet, "/api/v1/alerts/errors/info", "", http.StatusOK, `"actionTypes":["email","webhook"]`},
		{"info on an unknown alert", http.MethodGet, "/api/v1/alerts/nope/info", "", http.StatusNotFound, "alert not found"},
		{"info with a bad id", http.MethodGet, "/api/v1/alerts/a%20b/info", "", http.StatusBadRequest, "1-128 characters"},
		{"test its own trigger", http.MethodPost, "/api/v1/alerts/errors/test", "", http.StatusOK,
			`{"matches":true,"enabled":true,"event":"message.errored","flowId":"adt","actions":[{"type":"email","to":["ops@example.com"]}],"delivered":false}`},
		{"another flow", http.MethodPost, "/api/v1/alerts/errors/test", `{"flowId":"lab"}`, http.StatusOK, `"matches":false`},
		{"another event", http.MethodPost, "/api/v1/alerts/errors/test", `{"event":"message.queued"}`, http.StatusOK, `"actions":[]`},
		{"every flow", http.MethodPost, "/api/v1/alerts/any/test", `{"flowId":"lab"}`, http.StatusOK, `"matches":true,"enabled":false`},
		{"an unknown event", http.MethodPost, "/api/v1/alerts/errors/test", `{"event":"boom"}`, http.StatusBadRequest, "trigger events"},
		{"a bad flow id", http.MethodPost, "/api/v1/alerts/errors/test", `{"flowId":"a b"}`, http.StatusBadRequest, "flowId"},
		{"an unknown field", http.MethodPost, "/api/v1/alerts/errors/test", `{"flow":"adt"}`, http.StatusBadRequest, "unknown field"},
		{"test an unknown alert", http.MethodPost, "/api/v1/alerts/nope/test", "", http.StatusNotFound, "alert not found"},
		{"statuses is reserved", http.MethodPut, "/api/v1/alerts/statuses", strings.Replace(validAlert, `"id":"errors"`, `"id":"statuses"`, 1), http.StatusBadRequest, "reserved"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(s, tt.method, tt.path, tt.body, nil)
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("got %d %s; want %d containing %s", rec.Code, rec.Body.String(), tt.status, tt.want)
			}
		})
	}

	alerts.fail = true
	if rec := serve(s, http.MethodGet, "/api/v1/alerts/statuses", "", nil); rec.Code != http.StatusInternalServerError {
		t.Errorf("statuses on a failing store = %d", rec.Code)
	}
	for _, path := range []string{"/api/v1/alerts/statuses", "/api/v1/alerts/x/info"} {
		if rec := serve(New(Config{}), http.MethodGet, path, "", nil); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("GET %s without a store = %d", path, rec.Code)
		}
	}
}
