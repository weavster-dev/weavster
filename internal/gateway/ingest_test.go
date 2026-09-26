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

type fakeIngest struct{ err error }

func (f fakeIngest) Ingest(_ context.Context, flowID string, body []byte) (IngestResult, error) {
	if f.err != nil {
		return IngestResult{}, f.err
	}
	return IngestResult{ID: "m-" + flowID, Status: "sent"}, nil
}

func TestIngestHandler(t *testing.T) {
	tests := []struct {
		name   string
		ingest MessageIngester
		body   string
		want   int
		bodyIn string
	}{
		{"accepted", fakeIngest{}, `{}`, http.StatusAccepted, `"id":"m-f"`},
		{"unavailable", nil, `{}`, http.StatusServiceUnavailable, "unavailable"},
		{"unknown flow", fakeIngest{err: ErrFlowNotFound}, `{}`, http.StatusNotFound, "flow not found"},
		{"invalid message", fakeIngest{err: fmt.Errorf("%w: body must be a JSON object", ErrInvalidMessage)}, `x`, http.StatusBadRequest, "body must be a JSON object"},
		{"internal", fakeIngest{err: errors.New("disk full")}, `{}`, http.StatusInternalServerError, "internal error"},
		{"too large", fakeIngest{}, strings.Repeat("x", maxMessageBytes+1), http.StatusRequestEntityTooLarge, "larger than 10 MiB"},
		{"flow not running", fakeIngest{err: fmt.Errorf("%w: flow f is stopped", ErrFlowNotRunning)}, `{}`, http.StatusConflict, "flow f is stopped"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := New(Config{Ingest: tt.ingest}).Router()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/flows/f/messages", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			if rec.Code != tt.want || !strings.Contains(rec.Body.String(), tt.bodyIn) {
				t.Errorf("got %d %q, want %d containing %q", rec.Code, rec.Body.String(), tt.want, tt.bodyIn)
			}
		})
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

func TestIngestBodyReadError(t *testing.T) {
	srv := New(Config{Ingest: fakeIngest{}}).Router()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/flows/f/messages", errReader{})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("read error: %d, want 400", rec.Code)
	}
}

type fakeStats struct{ err error }

func (f fakeStats) FlowStats(context.Context, string, bool) (FlowStats, error) {
	return FlowStats{Received: 1}, f.err
}

type fakeEvents struct{ err error }

func (f fakeEvents) SearchEvents(context.Context, EventQuery) ([]Event, error) {
	return []Event{{ID: 1, Type: "message.sent"}}, f.err
}

func TestStatsAndEventsHandlers(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		path string
		want int
	}{
		{"stats", Config{Stats: fakeStats{}}, "/api/v1/flows/f/stats", http.StatusOK},
		{"stats unavailable", Config{}, "/api/v1/flows/f/stats", http.StatusServiceUnavailable},
		{"stats unknown flow", Config{Stats: fakeStats{err: ErrFlowNotFound}}, "/api/v1/flows/f/stats", http.StatusNotFound},
		{"events", Config{Events: fakeEvents{}}, "/api/v1/events", http.StatusOK},
		{"events unavailable", Config{}, "/api/v1/events", http.StatusServiceUnavailable},
		{"events error", Config{Events: fakeEvents{err: errors.New("boom")}}, "/api/v1/events", http.StatusInternalServerError},
		{"lifetime 1", Config{Stats: fakeStats{}}, "/api/v1/flows/f/stats?lifetime=1", http.StatusOK},
		{"bad lifetime", Config{Stats: fakeStats{}}, "/api/v1/flows/f/stats?lifetime=maybe", http.StatusBadRequest},
		{"events limit", Config{Events: fakeEvents{}}, "/api/v1/events?limit=5", http.StatusOK},
		{"bad limit", Config{Events: fakeEvents{}}, "/api/v1/events?limit=0", http.StatusBadRequest},
		{"limit too large", Config{Events: fakeEvents{}}, "/api/v1/events?limit=10001", http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(tt.cfg).Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if rec.Code != tt.want {
				t.Errorf("got %d %q, want %d", rec.Code, rec.Body.String(), tt.want)
			}
		})
	}
}

type fakeLifecycle struct{ err error }

func (f fakeLifecycle) Transition(_ context.Context, id, action string) (Flow, error) {
	return Flow{ID: id, Status: action + "ed"}, f.err
}

func (f fakeLifecycle) RedeployAll(context.Context) ([]Flow, error) { return []Flow{{ID: "a"}}, f.err }

func TestLifecycleHandlers(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		path string
		want int
	}{
		{"transition", Config{Lifecycle: fakeLifecycle{}}, "/api/v1/flows/f/deploy", http.StatusOK},
		{"unknown action", Config{Lifecycle: fakeLifecycle{}}, "/api/v1/flows/f/explode", http.StatusNotFound},
		{"unavailable", Config{}, "/api/v1/flows/f/start", http.StatusServiceUnavailable},
		{"invalid transition", Config{Lifecycle: fakeLifecycle{err: fmt.Errorf("%w: cannot pause a flow that is stopped", ErrInvalidTransition)}}, "/api/v1/flows/f/pause", http.StatusConflict},
		{"redeploy-all", Config{Lifecycle: fakeLifecycle{}}, "/api/v1/flows/redeploy-all", http.StatusOK},
		{"redeploy-all unavailable", Config{}, "/api/v1/flows/redeploy-all", http.StatusServiceUnavailable},
		{"redeploy-all error", Config{Lifecycle: fakeLifecycle{err: errors.New("boom")}}, "/api/v1/flows/redeploy-all", http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(tt.cfg).Router().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, tt.path, nil))
			if rec.Code != tt.want {
				t.Errorf("got %d %q, want %d", rec.Code, rec.Body.String(), tt.want)
			}
		})
	}
}
