package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/flowdef"
	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/observability"
)

// listedFlows lists fixed flows, or fails.
type listedFlows struct {
	flowLister
	flows []gateway.Flow
	err   error
}

func (l listedFlows) List(context.Context) ([]gateway.Flow, error) { return l.flows, l.err }

// TestServerMetrics: every existing flow is exported, with zeros before any
// traffic, for its destinations too; a deleted flow's leftover statistics
// are not; a failed flow list leaves the flow metrics out of that scrape;
// without a store there are no processing metrics.
func TestServerMetrics(t *testing.T) {
	stats := observability.NewStatsRegistry()
	stats.Inc("adt", observability.Received)
	stats.Inc("gone", observability.Received) // a flow deleted since
	flows := []gateway.Flow{
		{ID: "adt", Status: "started", Destinations: []flowdef.Destination{{Name: "ehr"}}},
		{ID: "idle", Status: "stopped"},
	}
	for _, tt := range []struct {
		name          string
		flows         listedFlows
		want, notWant []string
	}{
		{"flows listed", listedFlows{flows: flows},
			[]string{
				`weavster_flow_messages_total{flow="adt",outcome="received"} 1`,
				`weavster_flow_messages_total{flow="idle",outcome="received"} 0`,
				`weavster_connector_messages_total{connector="ehr",flow="adt",outcome="sent"} 0`,
				`weavster_flows{status="started"} 1`,
				`weavster_flows{status="stopped"} 1`,
			},
			[]string{`flow="gone"`, `connector="ehr",flow="adt",outcome="queued"`, "weavster_processing_"}},
		{"flows not listed", listedFlows{err: errors.New("store down")},
			nil,
			[]string{"weavster_flow_messages_total", "weavster_flows{", "weavster_processing_"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			m := serverMetrics{stats: stats, flows: tt.flows, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			metricsHandler(m).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
			body := rec.Body.String()
			for _, w := range tt.want {
				if !strings.Contains(body, w) {
					t.Errorf("missing %q", w)
				}
			}
			for _, w := range tt.notWant {
				if strings.Contains(body, w) {
					t.Errorf("unexpected %q", w)
				}
			}
		})
	}
}
