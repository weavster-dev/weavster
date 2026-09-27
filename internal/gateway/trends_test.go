package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fixedTrends reports one sent message in bucket 1 and records the query;
// fail makes it fail.
type fixedTrends struct {
	got  *MessageTrendQuery
	fail bool
}

func (f fixedTrends) MessageTrends(_ context.Context, q MessageTrendQuery) (map[int]map[string]int, error) {
	*f.got = q
	if f.fail {
		return nil, errDisk
	}
	return map[int]map[string]int{1: {"sent": 2, "errored": 1}}, nil
}

func TestMessageTrendsHandler(t *testing.T) {
	var got MessageTrendQuery
	ok := Config{Trends: fixedTrends{got: &got}}
	for _, tt := range []struct {
		name, query string
		cfg         Config
		status      int
		want        string
	}{
		{"hours", "?from=2026-09-27T10:00:00Z&to=2026-09-27T12:30:00Z&flowId=a", ok, http.StatusOK,
			`[{"start":"2026-09-27T10:00:00Z","total":0,"statuses":{"dead-lettered":0,"errored":0,"filtered":0,"queued":0,"received":0,"sent":0,"transformed":0}},{"start":"2026-09-27T11:00:00Z","total":3,`},
		{"days", "?from=2026-09-01T00:00:00Z&to=2026-09-08T00:00:00Z&interval=day", ok, http.StatusOK, `"start":"2026-09-07T00:00:00Z"`},
		{"missing range", "", ok, http.StatusBadRequest, "from and to are required"},
		{"to before from", "?from=2026-09-27T12:00:00Z&to=2026-09-27T12:00:00Z", ok, http.StatusBadRequest, "to must be after from"},
		{"bad time", "?from=x&to=y", ok, http.StatusBadRequest, "RFC 3339"},
		{"bad interval", "?from=2026-09-27T10:00:00Z&to=2026-09-27T12:00:00Z&interval=week", ok, http.StatusBadRequest, "hour or day"},
		{"too many buckets", "?from=2026-01-01T00:00:00Z&to=2026-03-01T00:00:00Z", ok, http.StatusBadRequest, "at most 1000"},
		{"fails", "?from=2026-09-27T10:00:00Z&to=2026-09-27T11:00:00Z", Config{Trends: fixedTrends{got: &got, fail: true}}, http.StatusInternalServerError, "internal error"},
		{"unavailable", "?from=2026-09-27T10:00:00Z&to=2026-09-27T11:00:00Z", Config{}, http.StatusServiceUnavailable, "unavailable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(tt.cfg).Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/messages/trends"+tt.query, nil))
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("got %d %.400s; want %d containing %q", rec.Code, rec.Body.String(), tt.status, tt.want)
			}
		})
	}
	if got.FlowID != "a" && got.Interval == 0 {
		t.Errorf("query = %+v", got)
	}
}
