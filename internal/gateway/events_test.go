package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEventHandlers(t *testing.T) {
	ok, failing := Config{Events: fakeEvents{}}, Config{Events: fakeEvents{err: errDisk}}
	for _, tt := range []struct {
		name, path string
		cfg        Config
		status     int
		want       string
	}{
		{"search with filters", "/api/v1/events?from=2026-09-26T00:00:00Z&to=2026-09-27T00:00:00+02:00&afterId=3", ok, http.StatusOK, `"id":1`},
		{"bad from", "/api/v1/events?from=today", ok, http.StatusBadRequest, "RFC 3339"},
		{"bad afterId", "/api/v1/events?afterId=-1", ok, http.StatusBadRequest, "afterId must be"},
		{"afterId too large", "/api/v1/events?afterId=99999999999999999999", ok, http.StatusBadRequest, "whole number from 0 to 9223372036854775807"},
		{"from after to", "/api/v1/events?from=2026-09-27T00:00:00Z&to=2026-09-26T00:00:00Z", ok, http.StatusBadRequest, "from must not be after to"},
		{"bad limit", "/api/v1/events?limit=0", ok, http.StatusBadRequest, "limit must be"},
		{"get", "/api/v1/events/1", ok, http.StatusOK, `"type":"message.sent"`},
		{"get unknown", "/api/v1/events/9", ok, http.StatusNotFound, "event not found"},
		{"get bad id", "/api/v1/events/x", ok, http.StatusBadRequest, "positive number"},
		{"get fails", "/api/v1/events/1", failing, http.StatusInternalServerError, "internal error"},
		{"count", "/api/v1/events/count?type=message.sent", ok, http.StatusOK, `{"count":1}`},
		{"count bad", "/api/v1/events/count?to=x", ok, http.StatusBadRequest, "RFC 3339"},
		{"count fails", "/api/v1/events/count", failing, http.StatusInternalServerError, "internal error"},
		{"max id", "/api/v1/events/max-id", ok, http.StatusOK, `{"maxId":1}`},
		{"max id fails", "/api/v1/events/max-id", failing, http.StatusInternalServerError, "internal error"},
		{"export", "/api/v1/events/export?flowId=f", ok, http.StatusOK, `[{"id":1`},
		{"export bad", "/api/v1/events/export?afterId=x", ok, http.StatusBadRequest, "afterId must be"},
		{"export fails", "/api/v1/events/export", failing, http.StatusInternalServerError, "internal error"},
		{"get unavailable", "/api/v1/events/1", Config{}, http.StatusServiceUnavailable, "unavailable"},
		{"count unavailable", "/api/v1/events/count", Config{}, http.StatusServiceUnavailable, "unavailable"},
		{"max id unavailable", "/api/v1/events/max-id", Config{}, http.StatusServiceUnavailable, "unavailable"},
		{"export unavailable", "/api/v1/events/export", Config{}, http.StatusServiceUnavailable, "unavailable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(tt.cfg).Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("got %d %s; want %d containing %q", rec.Code, rec.Body.String(), tt.status, tt.want)
			}
			if tt.name == "export" && !strings.Contains(rec.Header().Get("Content-Disposition"), "events.json") {
				t.Error("export is not a download")
			}
		})
	}
}
