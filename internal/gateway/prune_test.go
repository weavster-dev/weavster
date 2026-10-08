package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakePruner answers with fixed errors.
type fakePruner struct{ start, stop error }

func (fakePruner) Status() PruneStatus { return PruneStatus{MaxMessages: 5, IntervalMinutes: 60} }
func (p fakePruner) Start() error      { return p.start }
func (p fakePruner) Stop() error       { return p.stop }

func TestPruneHandlers(t *testing.T) {
	for _, tt := range []struct {
		name, method, path string
		pruner             Pruner
		status             int
		body               string
	}{
		{"status", http.MethodGet, "/api/v1/system/prune", fakePruner{}, http.StatusOK, `"maxMessages":5`},
		{"start", http.MethodPost, "/api/v1/system/prune/start", fakePruner{}, http.StatusAccepted, `"intervalMinutes":60`},
		{"start while running", http.MethodPost, "/api/v1/system/prune/start", fakePruner{start: ErrPruneRunning}, http.StatusConflict, "already running"},
		{"start with pruning off", http.MethodPost, "/api/v1/system/prune/start", fakePruner{start: ErrPruneOff}, http.StatusConflict, "not configured"},
		{"start while the server starts or stops", http.MethodPost, "/api/v1/system/prune/start", fakePruner{start: ErrPruneUnavailable}, http.StatusServiceUnavailable, "starts or stops"},
		{"stop", http.MethodPost, "/api/v1/system/prune/stop", fakePruner{}, http.StatusOK, `"running":false`},
		{"stop with nothing running", http.MethodPost, "/api/v1/system/prune/stop", fakePruner{stop: ErrPruneNotRunning}, http.StatusConflict, "no prune pass is running"},
		{"status without a store", http.MethodGet, "/api/v1/system/prune", nil, http.StatusServiceUnavailable, "messages unavailable"},
		{"start without a store", http.MethodPost, "/api/v1/system/prune/start", nil, http.StatusServiceUnavailable, "messages unavailable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(Config{Pruner: tt.pruner}).Router().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.body) {
				t.Errorf("got %d %s; want %d containing %s", rec.Code, rec.Body.String(), tt.status, tt.body)
			}
		})
	}
}
