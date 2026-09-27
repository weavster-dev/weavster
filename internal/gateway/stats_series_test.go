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
	got *StatsSeriesQuery
	err error
}

func (f fixedHistory) StatsSeries(_ context.Context, q StatsSeriesQuery) ([]StatsSample, error) {
	*f.got = q
	if f.err != nil {
		return nil, f.err
	}
	return []StatsSample{{At: time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC), FlowID: "a", Stats: FlowStats{Received: 3}}}, nil
}

func TestStatsSeriesHandler(t *testing.T) {
	var got StatsSeriesQuery
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
		{"zero limit", "?limit=0", ok, http.StatusBadRequest, "limit must be between 1 and 10000"},
		{"huge limit", "?limit=10001", ok, http.StatusBadRequest, "limit must be between 1 and 10000"},
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
	for query, want := range map[string]StatsSeriesQuery{
		"?flowId=a&from=2026-09-27T10:00:00Z": {FlowID: "a", From: time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC), Limit: DefaultStatsSeriesLimit},
		"?limit=10000":                        {Limit: 10000},
	} {
		New(ok).Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/stats/series"+query, nil))
		if got != want {
			t.Errorf("%s: query = %+v", query, got)
		}
	}
}
