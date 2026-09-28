package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestProcessingOrder: the flow's steps run in the order written on the
// input, each destination's steps then run on the flow's output, and the
// response transform runs last on the selected destination's reply.
func TestProcessingOrder(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	ehr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"OK","echo":` + string(b) + `}`))
	}))
	defer ehr.Close()
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)

	// The filter reads "adult", which the map step before it sets: written
	// in this order, adults pass. The destination filter reads "adult" too
	// (the flow's output, not the input); the response transform reads the
	// EHR's reply.
	mapThenFilter := `[{"map":{"from":"age.flag","to":"adult"}},{"filter":{"when":"adult == 'y'","action":"accept"}}]`
	createFlow(t, c, `{"id":"ordered","responseSelector":"ehr","transform":{"steps":`+mapThenFilter+`},"destinations":[`+
		`{"name":"ehr","type":"http","url":"`+ehr.URL+`",`+
		`"transform":{"steps":[{"filter":{"when":"adult == 'y'","action":"accept"}},{"set":{"field":"seenBy","expr":"ehr after {{adult}}"}}]},`+
		`"responseTransform":{"steps":[{"map":{"from":"code","to":"ack"}},{"map":{"from":"echo.seenBy","to":"seen"}}]}}]}`)
	code, resp, _ := c.do(http.MethodPost, "/api/v1/flows/ordered/messages", `{"age":{"flag":"y"}}`, admin)
	var res struct {
		Status   string
		Response map[string]any
	}
	_ = json.Unmarshal([]byte(resp), &res)
	if code != http.StatusAccepted || res.Status != "sent" {
		t.Fatalf("send: %d %s", code, resp)
	}
	mu.Lock()
	delivered := strings.Join(bodies, "|")
	mu.Unlock()
	if delivered != `{"adult":"y","age":{"flag":"y"},"seenBy":"ehr after y"}` {
		t.Errorf("delivered %s", delivered)
	}
	if res.Response["ack"] != "OK" || res.Response["seen"] != "ehr after y" {
		t.Errorf("response = %v", res.Response)
	}

	// The same steps the other way round: the filter runs before "adult"
	// exists, so the message is filtered.
	filterThenMap := `[{"filter":{"when":"adult == 'y'","action":"accept"}},{"map":{"from":"age.flag","to":"adult"}}]`
	createFlow(t, c, `{"id":"reordered","transform":{"steps":`+filterThenMap+`},"destinations":[{"name":"ehr","type":"http","url":"`+ehr.URL+`"}]}`)
	if _, status := sendMessage(t, c, "reordered", `{"age":{"flag":"y"}}`); status != "filtered" {
		t.Errorf("reordered steps: status %s, want filtered", status)
	}
	mu.Lock()
	n := len(bodies)
	mu.Unlock()
	if n != 1 {
		t.Errorf("the EHR received %d messages, want 1", n)
	}
}
