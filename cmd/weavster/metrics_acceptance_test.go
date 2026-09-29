package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestOperationalMetrics: after real traffic (a message delivered, one
// filtered, one queued for a failing destination, and one that fails its
// transform), GET /metrics reports the same counts as the flow statistics
// and the events, and it needs credentials.
func TestOperationalMetrics(t *testing.T) {
	var fail atomic.Bool
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer downstream.Close()
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\n"+storeConfig(t)+"delivery: {backoffBaseMs: 3600000}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)

	createFlow(t, c, `{"id":"lab","transform":{"name":"t","steps":[{"filter":{"when":"skip","action":"reject"}}]},
	  "destinations":[{"name":"ehr","type":"http","url":"`+downstream.URL+`"}]}`)
	createFlow(t, c, `{"id":"conv","transform":{"name":"t","steps":[{"map":{"from":"n","to":"n","type":"number"}}]}}`)
	c.do(http.MethodPost, "/api/v1/flows/lab/messages", `{"n":1}`, admin)       // sent
	c.do(http.MethodPost, "/api/v1/flows/lab/messages", `{"skip":true}`, admin) // filtered
	fail.Store(true)
	c.do(http.MethodPost, "/api/v1/flows/lab/messages", `{"n":2}`, admin)    // queued
	c.do(http.MethodPost, "/api/v1/flows/conv/messages", `{"n":"x"}`, admin) // errored in the transform

	resp, err := http.Get("http://" + addr + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("metrics without credentials: %d, want 401", resp.StatusCode)
	}
	code, metrics, header := c.do(http.MethodGet, "/metrics", "", admin)
	if code != http.StatusOK || !strings.HasPrefix(header.Get("Content-Type"), "text/plain") {
		t.Fatalf("metrics: %d %s", code, header.Get("Content-Type"))
	}
	for _, want := range []string{
		`weavster_flow_messages_total{flow="lab",outcome="received"} 3`,
		`weavster_flow_messages_total{flow="lab",outcome="sent"} 1`,
		`weavster_flow_messages_total{flow="lab",outcome="filtered"} 1`,
		`weavster_flow_messages_total{flow="lab",outcome="queued"} 1`,
		`weavster_flow_messages_total{flow="conv",outcome="errored"} 1`,
		`weavster_connector_messages_total{connector="ehr",flow="lab",outcome="sent"} 1`,
		`weavster_connector_messages_total{connector="ehr",flow="lab",outcome="errored"} 1`,
		`weavster_flows{status="started"} 2`,
		`weavster_processing_in_flight 0`,
		`weavster_processing_slots 32`,
		`weavster_processing_refused_total 0`,
		`go_goroutines `, // the Go runtime; process_* metrics depend on the OS
	} {
		if !strings.Contains(metrics, want) {
			t.Errorf("metrics lack %q", want)
		}
	}

	// The flow statistics say the same.
	var st struct{ Received, Filtered, Sent, Queued, Errored int64 }
	_, body, _ := c.do(http.MethodGet, "/api/v1/flows/lab/stats?lifetime=true", "", admin)
	if err := json.Unmarshal([]byte(body), &st); err != nil || st.Received != 3 || st.Sent != 1 || st.Filtered != 1 || st.Queued != 1 {
		t.Errorf("lab lifetime stats = %s", body)
	}
	_, body, _ = c.do(http.MethodGet, "/api/v1/flows/conv/stats?lifetime=true", "", admin)
	if err := json.Unmarshal([]byte(body), &st); err != nil || st.Errored != 1 {
		t.Errorf("conv lifetime stats = %s", body)
	}
	// And so do the events.
	for typ, want := range map[string]string{"message.queued": "1", "message.errored": "1"} {
		if _, body, _ := c.do(http.MethodGet, "/api/v1/events/count?type="+typ, "", admin); !strings.Contains(body, `"count":`+want) {
			t.Errorf("%s events: %s, want %s", typ, body, want)
		}
	}
}
