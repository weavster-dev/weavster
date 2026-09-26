package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/observability"
	"github.com/weavster-dev/weavster/internal/pipeline"
	"github.com/weavster-dev/weavster/internal/serverconfig"
	"github.com/weavster-dev/weavster/internal/state"
)

// TestStatsEventsTopology proves statistics, events, and topology activity
// reflect real processing: one sent, one filtered, and one queued message.
func TestStatsEventsTopology(t *testing.T) {
	fail := false
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if fail {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer downstream.Close()

	c := startComposed(t, serverconfig.Default(), io.Discard)
	admin := basic(bootstrapAdmin, testAdminPassword)
	if status, body, _ := c.do(http.MethodGet, "/api/v1/events", "", admin); status != http.StatusOK || strings.TrimSpace(body) != "[]" {
		t.Errorf("events on a fresh server: %d %q, want 200 []", status, body)
	}
	flow := `{"id":"lab","name":"Lab","transform":{"name":"t","steps":[{"filter":{"when":"skip","action":"reject"}}]},
	  "destinations":[{"name":"ehr","type":"http","url":"` + downstream.URL + `"}]}`
	if status, body, _ := c.do(http.MethodPost, "/api/v1/flows", flow, admin); status != http.StatusCreated {
		t.Fatalf("create flow: %d %q", status, body)
	}

	// Zeros before any traffic.
	status, body, _ := c.do(http.MethodGet, "/api/v1/flows/lab/stats", "", admin)
	if status != http.StatusOK || !strings.Contains(body, `"received":0`) || !strings.Contains(body, `"lastMessageAt":null`) {
		t.Errorf("empty stats: %d %s", status, body)
	}

	c.do(http.MethodPost, "/api/v1/flows/lab/messages", `{"n":1}`, admin)       // sent
	c.do(http.MethodPost, "/api/v1/flows/lab/messages", `{"skip":true}`, admin) // filtered
	fail = true
	c.do(http.MethodPost, "/api/v1/flows/lab/messages", `{"n":2}`, admin) // queued

	var st struct {
		Received, Filtered, Transformed, Sent, Errored, Queued int64
		Destinations                                           map[string]struct{ Sent, Errored int64 }
		LastMessageAt                                          *string
	}
	_, body, _ = c.do(http.MethodGet, "/api/v1/flows/lab/stats", "", admin)
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatal(err)
	}
	if st.Received != 3 || st.Filtered != 1 || st.Transformed != 2 || st.Sent != 1 || st.Queued != 1 || st.Errored != 0 ||
		st.Destinations["ehr"].Sent != 1 || st.Destinations["ehr"].Errored != 1 || st.LastMessageAt == nil {
		t.Errorf("stats = %s", body)
	}
	if status, _, _ := c.do(http.MethodGet, "/api/v1/flows/lab/stats?lifetime=true", "", admin); status != http.StatusOK {
		t.Errorf("lifetime stats: %d", status)
	}
	if status, _, _ := c.do(http.MethodGet, "/api/v1/flows/nope/stats", "", admin); status != http.StatusNotFound {
		t.Errorf("unknown flow stats: %d, want 404", status)
	}

	_, body, _ = c.do(http.MethodGet, "/api/v1/events?flowId=lab", "", admin)
	for _, typ := range []string{`"type":"message.sent"`, `"type":"message.filtered"`, `"type":"message.queued"`} {
		if !strings.Contains(body, typ) {
			t.Errorf("events missing %s: %s", typ, body)
		}
	}
	_, body, _ = c.do(http.MethodGet, "/api/v1/events?type=message.queued", "", admin)
	var events []map[string]any
	if err := json.Unmarshal([]byte(body), &events); err != nil || len(events) != 1 {
		t.Errorf("queued events = %s", body)
	}

	_, body, _ = c.do(http.MethodGet, "/api/v1/topology", "", admin)
	if !strings.Contains(body, `"received":3`) || !strings.Contains(body, `"queued":1`) || !strings.Contains(body, `"lastMessageAt":"`) {
		t.Errorf("topology activity = %s", body)
	}

	// A transform error's text can quote message content, so the event
	// carries only the message id.
	if status, body, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"conv","transform":{"name":"t","steps":[{"map":{"from":"name","to":"n","type":"number"}}]}}`, admin); status != http.StatusCreated {
		t.Fatalf("create conv: %d %q", status, body)
	}
	c.do(http.MethodPost, "/api/v1/flows/conv/messages", `{"name":"John Smith"}`, admin)
	_, body, _ = c.do(http.MethodGet, "/api/v1/events?flowId=conv&type=message.errored", "", admin)
	if !strings.Contains(body, `"messageId"`) || strings.Contains(body, "John Smith") {
		t.Errorf("errored event = %s; want the message id and no message content", body)
	}
	if _, body, _ := c.do(http.MethodGet, "/api/v1/events?limit=1", "", admin); strings.Count(body, `"id":`) != 1 {
		t.Errorf("limit=1 returned %s", body)
	}

	// Deleting and re-creating a flow starts its stats from zero; an idle
	// flow's topology activity shows zeros.
	c.do(http.MethodDelete, "/api/v1/flows/lab", "", admin)
	c.do(http.MethodPost, "/api/v1/flows", `{"id":"lab","name":"Lab"}`, admin)
	if _, body, _ := c.do(http.MethodGet, "/api/v1/flows/lab/stats", "", admin); !strings.Contains(body, `"received":0`) {
		t.Errorf("stats after re-create = %s", body)
	}
	if _, body, _ := c.do(http.MethodGet, "/api/v1/topology", "", admin); !strings.Contains(body, `"activity":{"received":0,"sent":0,"errored":0,"queued":0}`) {
		t.Errorf("idle topology activity = %s", body)
	}
}

type blockingSink struct{ release chan struct{} }

func (s blockingSink) Write(context.Context, pipeline.Delivery) error {
	<-s.release
	return nil
}

// TestDeleteWaitsForInFlightIngest proves a flow delete is ordered after
// in-flight processing, so the deleted flow's counters stay reset.
func TestDeleteWaitsForInFlightIngest(t *testing.T) {
	ctx := context.Background()
	store := state.NewMemStore()
	stats := observability.NewStatsRegistry()
	flows := flowAdapter{store: store, stats: stats, gate: &sync.RWMutex{}}
	if err := flows.Create(ctx, gateway.Flow{ID: "f", Destinations: []gateway.FlowDestination{{Name: "d", Type: "file", Dir: t.TempDir()}}}); err != nil {
		t.Fatal(err)
	}
	sink := blockingSink{release: make(chan struct{})}
	ingest := ingestAdapter{flows: flows, pipe: pipeline.New(store, func(pipeline.Destination) (pipeline.Sink, error) { return sink, nil },
		processingObserver{stats, observability.NewEventLog()}, pipeline.Options{})}

	ingested := make(chan struct{})
	go func() {
		_, _ = ingest.Ingest(ctx, "f", []byte("x"))
		close(ingested)
	}()
	for stats.Snapshot("f", false).Received == 0 { // wait until processing is in flight
		time.Sleep(time.Millisecond)
	}
	deleted := make(chan error, 1)
	go func() { deleted <- flows.Delete(ctx, "f") }()
	select {
	case <-deleted:
		t.Fatal("delete completed while a message was still being processed")
	case <-time.After(50 * time.Millisecond):
	}
	close(sink.release)
	<-ingested
	if err := <-deleted; err != nil {
		t.Fatal(err)
	}
	if s := stats.Snapshot("f", false); s.Received != 0 || s.Sent != 0 {
		t.Errorf("stats after delete = %+v, want reset", s)
	}
}
