package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/serverconfig"
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
}
