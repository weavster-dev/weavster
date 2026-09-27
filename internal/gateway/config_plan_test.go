package gateway

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakePlanner plans "ok" (reporting how many live flows it saw), rejects
// "bad", and fails on anything else.
type fakePlanner struct{}

func (fakePlanner) PlanConfig(doc []byte, live ConfigBundle) (ConfigPlan, error) {
	switch string(doc) {
	case "ok":
		return ConfigPlan{Fingerprint: "f", Added: []string{"flow/a"}, Unchanged: len(live.Flows), Text: "+ flow/a\n"}, nil
	case "changed":
		return ConfigPlan{Fingerprint: "c", Changes: []ConfigChange{{Key: "flow/a", Action: "add"}}}, nil
	case "bad":
		return ConfigPlan{}, fmt.Errorf("%w: config: flows.a: bad", ErrInvalidConfig)
	}
	return ConfigPlan{}, errors.New("disk")
}

func TestConfigPlanHandler(t *testing.T) {
	stores := func(transfer FlowTransfer) Config {
		return Config{ConfigPlanner: fakePlanner{}, Transfer: transfer, Alerts: &memAlerts{alerts: map[string]Alert{}},
			Snippets: &memSnippets{snippets: map[string]Snippet{}, libraries: map[string]SnippetLibrary{}}, Items: memItems{}}
	}
	for _, tt := range []struct {
		name, body string
		cfg        Config
		status     int
		want       string
	}{
		{"plan", "ok", stores(fakeTransfer{}), http.StatusOK, `"unchanged":1`},
		{"invalid", "bad", stores(fakeTransfer{}), http.StatusBadRequest, "config: flows.a: bad"},
		{"planner fails", "x", stores(fakeTransfer{}), http.StatusInternalServerError, "internal error"},
		{"live read fails", "ok", stores(fakeTransfer{err: errDisk}), http.StatusInternalServerError, "internal error"},
		{"too large", strings.Repeat("x", maxImportBytes+1), stores(fakeTransfer{}), http.StatusRequestEntityTooLarge, "50 MiB"},
		{"no planner", "ok", Config{}, http.StatusServiceUnavailable, "planning unavailable"},
		{"no stores", "ok", Config{ConfigPlanner: fakePlanner{}}, http.StatusServiceUnavailable, "unavailable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(tt.cfg).Router().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/config/plan", strings.NewReader(tt.body)))
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("got %d %.200q; want %d containing %q", rec.Code, rec.Body.String(), tt.status, tt.want)
			}
		})
	}
}
