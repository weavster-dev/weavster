package gateway

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeValidator accepts documents that say "ok".
type fakeValidator struct{}

func (fakeValidator) ValidateConfig(doc []byte) (ConfigSummary, error) {
	if string(doc) != "ok" {
		return ConfigSummary{}, errors.New("config: flows.a: bad")
	}
	return ConfigSummary{Flows: 2}, nil
}

func TestConfigValidateHandler(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		cfg        Config
		status     int
		want       string
	}{
		{"valid", "ok", Config{ConfigValidator: fakeValidator{}}, http.StatusOK, `{"counts":{"flows":2,`},
		{"invalid", "bad", Config{ConfigValidator: fakeValidator{}}, http.StatusBadRequest, "config: flows.a: bad"},
		{"too large", strings.Repeat("x", maxImportBytes+1), Config{ConfigValidator: fakeValidator{}}, http.StatusRequestEntityTooLarge, "50 MiB"},
		{"unavailable", "ok", Config{}, http.StatusServiceUnavailable, "unavailable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(tt.cfg).Router().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/config/validate", strings.NewReader(tt.body)))
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("got %d %.200q; want %d containing %q", rec.Code, rec.Body.String(), tt.status, tt.want)
			}
		})
	}
}
