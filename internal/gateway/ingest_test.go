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
		{"too large", fakeIngest{}, strings.Repeat("x", MaxMessageBytes+1), http.StatusRequestEntityTooLarge, "larger than 10 MiB"},
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

func (f fakeStats) AllFlowStats(context.Context, bool) (map[string]FlowStats, error) {
	return map[string]FlowStats{"a": {Received: 1}}, f.err
}
func (f fakeStats) ResetStats(context.Context, string, bool) error { return f.err }
func (f fakeStats) FlowStats(context.Context, string, bool) (FlowStats, error) {
	return FlowStats{Received: 1}, f.err
}

type fakeEvents struct{ err error }

func (f fakeEvents) SearchEvents(context.Context, EventQuery) ([]Event, error) {
	return []Event{{ID: 1, Type: "message.sent"}}, f.err
}
func (f fakeEvents) GetEvent(_ context.Context, id int64) (Event, error) {
	if id != 1 {
		return Event{}, ErrEventNotFound
	}
	return Event{ID: 1, Type: "message.sent"}, f.err
}
func (f fakeEvents) CountEvents(context.Context, EventQuery) (int, error) { return 1, f.err }
func (f fakeEvents) MaxEventID(context.Context) (int64, error)            { return 1, f.err }

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
func (f fakeLifecycle) TransitionAll(_ context.Context, action string) (TransitionAllResult, error) {
	return TransitionAllResult{Changed: []string{"a"}, Skipped: []SkippedFlow{{ID: "b", Reason: action}}}, f.err
}
func (f fakeLifecycle) SetDestinationRunning(_ context.Context, id, dest string, running bool) (Flow, error) {
	return Flow{ID: id}, f.err
}

func TestLifecycleHandlers(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		path string
		want int
	}{
		{"transition", Config{Lifecycle: fakeLifecycle{}}, "/api/v1/flows/f/deploy", http.StatusOK},
		{"unknown action", Config{Lifecycle: fakeLifecycle{err: fmt.Errorf("%w: explode", ErrUnknownAction)}}, "/api/v1/flows/f/explode", http.StatusNotFound},
		{"unavailable", Config{}, "/api/v1/flows/f/start", http.StatusServiceUnavailable},
		{"invalid transition", Config{Lifecycle: fakeLifecycle{err: fmt.Errorf("%w: cannot pause a flow that is stopped", ErrInvalidTransition)}}, "/api/v1/flows/f/pause", http.StatusConflict},
		{"redeploy-all", Config{Lifecycle: fakeLifecycle{}}, "/api/v1/flows/redeploy-all", http.StatusOK},
		{"destination stop", Config{Lifecycle: fakeLifecycle{}}, "/api/v1/flows/f/destinations/d/stop", http.StatusOK},
		{"destination start", Config{Lifecycle: fakeLifecycle{}}, "/api/v1/flows/f/destinations/d/start", http.StatusOK},
		{"destination bad action", Config{Lifecycle: fakeLifecycle{}}, "/api/v1/flows/f/destinations/d/explode", http.StatusNotFound},
		{"destination unavailable", Config{}, "/api/v1/flows/f/destinations/d/stop", http.StatusServiceUnavailable},
		{"destination unknown", Config{Lifecycle: fakeLifecycle{err: ErrDestinationNotFound}}, "/api/v1/flows/f/destinations/d/stop", http.StatusNotFound},
		{"redeploy-all unavailable", Config{}, "/api/v1/flows/redeploy-all", http.StatusServiceUnavailable},
		{"redeploy-all partial", Config{Lifecycle: fakeLifecycle{err: errors.New("boom")}}, "/api/v1/flows/redeploy-all", http.StatusInternalServerError},
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

func TestRedeployAllPartialBody(t *testing.T) {
	rec := httptest.NewRecorder()
	New(Config{Lifecycle: fakeLifecycle{err: errors.New("database is locked")}}).Router().
		ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/flows/redeploy-all", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "REDEPLOY_INCOMPLETE") || !strings.Contains(body, `"redeployed":[{"id":"a"`) || strings.Contains(body, "database is locked") {
		t.Errorf("partial redeploy body = %s", body)
	}
}

type fakeUpdater struct{ err error }

func (f fakeUpdater) Update(_ context.Context, id string, fl Flow, _ bool) (Flow, error) {
	return fl, f.err
}
func (f fakeUpdater) SetEnabled(_ context.Context, id string, enabled bool) (Flow, error) {
	return Flow{ID: id, Enabled: enabled}, f.err
}
func (f fakeUpdater) UpdateMany(_ context.Context, changes []FlowChange) ([]string, error) {
	var ids []string
	for _, c := range changes {
		if !c.KeepEnabled {
			ids = append(ids, c.Flow.ID+":enabled")
			continue
		}
		ids = append(ids, c.Flow.ID)
	}
	return ids, f.err
}

func TestFlowUpdateHandlers(t *testing.T) {
	tests := []struct {
		name, method, path, body string
		cfg                      Config
		want                     int
	}{
		{"update", http.MethodPut, "/api/v1/flows/f", `{"name":"x"}`, Config{FlowUpdates: fakeUpdater{}}, http.StatusOK},
		{"update unavailable", http.MethodPut, "/api/v1/flows/f", `{}`, Config{}, http.StatusServiceUnavailable},
		{"update bad json", http.MethodPut, "/api/v1/flows/f", `x`, Config{FlowUpdates: fakeUpdater{}}, http.StatusBadRequest},
		{"update null body", http.MethodPut, "/api/v1/flows/f", `null`, Config{FlowUpdates: fakeUpdater{}}, http.StatusBadRequest},
		{"update unknown", http.MethodPut, "/api/v1/flows/f", `{}`, Config{FlowUpdates: fakeUpdater{err: ErrFlowNotFound}}, http.StatusNotFound},
		{"enable", http.MethodPost, "/api/v1/flows/f/enable", ``, Config{FlowUpdates: fakeUpdater{}}, http.StatusOK},
		{"disable unavailable", http.MethodPost, "/api/v1/flows/f/disable", ``, Config{}, http.StatusServiceUnavailable},
		{"enable unknown", http.MethodPost, "/api/v1/flows/f/enable", ``, Config{FlowUpdates: fakeUpdater{err: ErrFlowNotFound}}, http.StatusNotFound},
		{"bulk update", http.MethodPut, "/api/v1/flows", `{"flows":[{"id":"a"}]}`, Config{FlowUpdates: fakeUpdater{}}, http.StatusOK},
		{"start all", http.MethodPost, "/api/v1/flows/start-all", ``, Config{Lifecycle: fakeLifecycle{}}, http.StatusOK},
		{"start all unavailable", http.MethodPost, "/api/v1/flows/start-all", ``, Config{}, http.StatusServiceUnavailable},
		{"start all incomplete", http.MethodPost, "/api/v1/flows/start-all", ``, Config{Lifecycle: fakeLifecycle{err: ErrTransitionIncomplete}}, http.StatusInternalServerError},
		{"start all store error", http.MethodPost, "/api/v1/flows/start-all", ``, Config{Lifecycle: fakeLifecycle{err: errors.New("disk")}}, http.StatusInternalServerError},
		{"all stats", http.MethodGet, "/api/v1/flows/stats", ``, Config{Stats: fakeStats{}}, http.StatusOK},
		{"all stats unavailable", http.MethodGet, "/api/v1/flows/stats", ``, Config{}, http.StatusServiceUnavailable},
		{"all stats bad lifetime", http.MethodGet, "/api/v1/flows/stats?lifetime=x", ``, Config{Stats: fakeStats{}}, http.StatusBadRequest},
		{"all stats error", http.MethodGet, "/api/v1/flows/stats", ``, Config{Stats: fakeStats{err: errors.New("disk")}}, http.StatusInternalServerError},
		{"reset stats", http.MethodPost, "/api/v1/flows/stats/reset?lifetime=true", ``, Config{Stats: fakeStats{}}, http.StatusNoContent},
		{"reset flow stats unknown", http.MethodPost, "/api/v1/flows/f/stats/reset", ``, Config{Stats: fakeStats{err: ErrFlowNotFound}}, http.StatusNotFound},
		{"reset stats unavailable", http.MethodPost, "/api/v1/flows/stats/reset", ``, Config{}, http.StatusServiceUnavailable},
		{"reset stats bad lifetime", http.MethodPost, "/api/v1/flows/stats/reset?lifetime=x", ``, Config{Stats: fakeStats{}}, http.StatusBadRequest},
		{"bulk update unavailable", http.MethodPut, "/api/v1/flows", `{"flows":[]}`, Config{}, http.StatusServiceUnavailable},
		{"bulk update missing flows", http.MethodPut, "/api/v1/flows", `{}`, Config{FlowUpdates: fakeUpdater{}}, http.StatusBadRequest},
		{"bulk update with version", http.MethodPut, "/api/v1/flows", `{"version":1,"flows":[]}`, Config{FlowUpdates: fakeUpdater{}}, http.StatusBadRequest},
		{"bulk update with null version", http.MethodPut, "/api/v1/flows", `{"version":null,"flows":[]}`, Config{FlowUpdates: fakeUpdater{}}, http.StatusBadRequest},
		{"bulk update unknown field", http.MethodPut, "/api/v1/flows", `{"overwrite":true,"flows":[]}`, Config{FlowUpdates: fakeUpdater{}}, http.StatusBadRequest},
		{"bulk update status", http.MethodPut, "/api/v1/flows", `{"flows":[{"id":"a","status":"started"}]}`, Config{FlowUpdates: fakeUpdater{}}, http.StatusBadRequest},
		{"bulk update bad initial state", http.MethodPut, "/api/v1/flows", `{"flows":[{"id":"a","initialState":"halted"}]}`, Config{FlowUpdates: fakeUpdater{}}, http.StatusBadRequest},
		{"bulk update unknown", http.MethodPut, "/api/v1/flows", `{"flows":[{"id":"a"}]}`, Config{FlowUpdates: fakeUpdater{err: ErrFlowNotFound}}, http.StatusNotFound},
		{"bulk update invalid", http.MethodPut, "/api/v1/flows", `{"flows":[{"id":"a"}]}`, Config{FlowUpdates: fakeUpdater{err: ErrInvalidFlow}}, http.StatusBadRequest},
		{"bulk update incomplete", http.MethodPut, "/api/v1/flows", `{"flows":[{"id":"a"}]}`, Config{FlowUpdates: fakeUpdater{err: ErrUpdateIncomplete}}, http.StatusInternalServerError},
		{"connector names", http.MethodGet, "/api/v1/flows/connector-names", ``, Config{Flows: &fakeFlows{}}, http.StatusOK},
		{"connector names unavailable", http.MethodGet, "/api/v1/flows/connector-names", ``, Config{}, http.StatusServiceUnavailable},
		{"connector names error", http.MethodGet, "/api/v1/flows/connector-names", ``, Config{Flows: &errFlows{}}, http.StatusInternalServerError},
		{"ports in use", http.MethodGet, "/api/v1/flows/ports-in-use", ``, Config{}, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(tt.cfg).Router().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))
			if rec.Code != tt.want {
				t.Errorf("got %d %q, want %d", rec.Code, rec.Body.String(), tt.want)
			}
		})
	}
}

type fakeTransfer struct{ err error }

func (f fakeTransfer) Export(context.Context, []string) ([]Flow, error) {
	return []Flow{{ID: "a"}}, f.err
}
func (f fakeTransfer) Import(context.Context, []Flow, bool) (ImportResult, error) {
	return ImportResult{Created: []string{"a"}}, f.err
}

func TestFlowOperationBodies(t *testing.T) {
	tests := []struct {
		name, method, path, body string
		cfg                      Config
		want                     string
	}{
		{"bulk update keeps enabled unless set", http.MethodPut, "/api/v1/flows", `{"flows":[{"id":"a"},{"id":"b","enabled":false}]}`,
			Config{FlowUpdates: fakeUpdater{}}, `{"updated":["a","b:enabled"]}`},
		{"bulk update incomplete lists written", http.MethodPut, "/api/v1/flows", `{"flows":[{"id":"a"}]}`,
			Config{FlowUpdates: fakeUpdater{err: ErrUpdateIncomplete}}, `"updated":["a"]`},
		{"connector names", http.MethodGet, "/api/v1/flows/connector-names", ``,
			Config{Flows: &fakeFlows{flows: []Flow{{ID: "f", Name: "F", SourceType: "http", Destinations: []FlowDestination{{Name: "a"}, {Name: "b"}}}, {ID: "g"}}}},
			`[{"id":"f","name":"F","sourceType":"http","destinations":["a","b"]},{"id":"g","name":"","sourceType":"","destinations":[]}]`},
		{"ports in use", http.MethodGet, "/api/v1/flows/ports-in-use", ``,
			Config{Listeners: []PortInUse{{Address: ":8080", Port: 8080, UsedBy: "api"}}}, `[{"address":":8080","port":8080,"usedBy":"api"}]`},
		{"no ports", http.MethodGet, "/api/v1/flows/ports-in-use", ``, Config{}, `[]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(tt.cfg).Router().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))
			if !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("body = %s, want it to contain %s", rec.Body.String(), tt.want)
			}
		})
	}
}

func TestTransferHandlers(t *testing.T) {
	tests := []struct {
		name, method, path, body string
		cfg                      Config
		want                     int
	}{
		{"export", http.MethodGet, "/api/v1/flows/export?ids=a", ``, Config{Transfer: fakeTransfer{}}, http.StatusOK},
		{"export unavailable", http.MethodGet, "/api/v1/flows/export", ``, Config{}, http.StatusServiceUnavailable},
		{"export unknown", http.MethodGet, "/api/v1/flows/export?ids=x", ``, Config{Transfer: fakeTransfer{err: ErrFlowNotFound}}, http.StatusNotFound},
		{"import", http.MethodPost, "/api/v1/flows/import", `{"version":1,"flows":[{"id":"a"}]}`, Config{Transfer: fakeTransfer{}}, http.StatusOK},
		{"import unavailable", http.MethodPost, "/api/v1/flows/import", `{}`, Config{}, http.StatusServiceUnavailable},
		{"import conflict", http.MethodPost, "/api/v1/flows/import", `{"version":1,"flows":[{"id":"a"}]}`, Config{Transfer: fakeTransfer{err: ErrImportConflict}}, http.StatusConflict},
		{"import bad flow", http.MethodPost, "/api/v1/flows/import", `{"version":1,"flows":[1]}`, Config{Transfer: fakeTransfer{}}, http.StatusBadRequest},
		{"import bad id", http.MethodPost, "/api/v1/flows/import", `{"version":1,"flows":[{"id":"import"}]}`, Config{Transfer: fakeTransfer{}}, http.StatusBadRequest},
		{"import missing flows", http.MethodPost, "/api/v1/flows/import", `{"version":1}`, Config{Transfer: fakeTransfer{}}, http.StatusBadRequest},
		{"import missing version", http.MethodPost, "/api/v1/flows/import", `{"flows":[]}`, Config{Transfer: fakeTransfer{}}, http.StatusBadRequest},
		{"import string version", http.MethodPost, "/api/v1/flows/import", `{"version":"1","flows":[]}`, Config{Transfer: fakeTransfer{}}, http.StatusBadRequest},
		{"import null flows", http.MethodPost, "/api/v1/flows/import", `{"version":1,"flows":null}`, Config{Transfer: fakeTransfer{}}, http.StatusBadRequest},
		{"import trailing data", http.MethodPost, "/api/v1/flows/import", `{"version":1,"flows":[]}{}`, Config{Transfer: fakeTransfer{}}, http.StatusBadRequest},
		{"import incomplete", http.MethodPost, "/api/v1/flows/import", `{"version":1,"flows":[{"id":"a"}]}`, Config{Transfer: fakeTransfer{err: ErrImportIncomplete}}, http.StatusInternalServerError},
		{"import too large", http.MethodPost, "/api/v1/flows/import", `{"version":1,"flows":["` + strings.Repeat("x", maxImportBytes) + `"]}`, Config{Transfer: fakeTransfer{}}, http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(tt.cfg).Router().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))
			if rec.Code != tt.want {
				t.Errorf("got %d %q, want %d", rec.Code, rec.Body.String(), tt.want)
			}
		})
	}
}
