package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fixedHistory returns one sample and records the query; err makes it fail.
type fixedHistory struct {
	got *[3]any
	err error
}

func (f fixedHistory) StatsSeries(_ context.Context, flowID string, from, to time.Time) ([]StatsSample, error) {
	*f.got = [3]any{flowID, from, to}
	if f.err != nil {
		return nil, f.err
	}
	return []StatsSample{{At: time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC), FlowID: "a", Stats: FlowStats{Received: 3}}}, nil
}

func TestStatsSeriesHandler(t *testing.T) {
	var got [3]any
	ok := Config{StatsHistory: fixedHistory{got: &got}}
	for _, tt := range []struct {
		name, query string
		cfg         Config
		status      int
		want        string
	}{
		{"all", "", ok, http.StatusOK, `[{"at":"2026-09-27T10:00:00Z","flowId":"a","stats":{"received":3,`},
		{"range and flow", "?flowId=a&from=2026-09-27T10:00:00Z&to=2026-09-27T11:00:00Z", ok, http.StatusOK, `"flowId":"a"`},
		{"bad time", "?from=yesterday", ok, http.StatusBadRequest, "RFC 3339"},
		{"reversed range", "?from=2026-09-27T11:00:00Z&to=2026-09-27T10:00:00Z", ok, http.StatusBadRequest, "from must not be after to"},
		{"unknown flow", "?flowId=nope", Config{StatsHistory: fixedHistory{got: &got, err: ErrFlowNotFound}}, http.StatusNotFound, "flow not found"},
		{"fails", "", Config{StatsHistory: fixedHistory{got: &got, err: errDisk}}, http.StatusInternalServerError, "internal error"},
		{"unavailable", "", Config{}, http.StatusServiceUnavailable, "statistics unavailable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(tt.cfg).Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/stats/series"+tt.query, nil))
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("got %d %.300s; want %d containing %q", rec.Code, rec.Body.String(), tt.status, tt.want)
			}
		})
	}
	// The filter and range reach the port.
	rec := httptest.NewRecorder()
	New(ok).Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/stats/series?flowId=a&from=2026-09-27T10:00:00Z", nil))
	if got[0] != "a" || !got[1].(time.Time).Equal(time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)) || !got[2].(time.Time).IsZero() {
		t.Errorf("query = %v", got)
	}
}
