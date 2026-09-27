package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakePlanner plans "ok", rejects "bad", and fails on anything else.
type fakePlanner struct{}

func (fakePlanner) PlanConfig(_ context.Context, doc []byte) (ConfigPlan, error) {
	switch string(doc) {
	case "ok":
		return ConfigPlan{Fingerprint: "f", Added: []string{"flow/a"}, Text: "+ flow/a\n"}, nil
	case "bad":
		return ConfigPlan{}, fmt.Errorf("%w: config: flows.a: bad", ErrInvalidConfig)
	}
	return ConfigPlan{}, errors.New("disk")
}

func TestConfigPlanHandler(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		cfg        Config
		status     int
		want       string
	}{
		{"plan", "ok", Config{ConfigPlanner: fakePlanner{}}, http.StatusOK, `"added":["flow/a"]`},
		{"invalid", "bad", Config{ConfigPlanner: fakePlanner{}}, http.StatusBadRequest, "config: flows.a: bad"},
		{"store fails", "x", Config{ConfigPlanner: fakePlanner{}}, http.StatusInternalServerError, "internal error"},
		{"too large", strings.Repeat("x", maxImportBytes+1), Config{ConfigPlanner: fakePlanner{}}, http.StatusRequestEntityTooLarge, "50 MiB"},
		{"unavailable", "ok", Config{}, http.StatusServiceUnavailable, "unavailable"},
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
