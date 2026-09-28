package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestPipelineEndToEnd starts `weavster server` with sqlite, creates a flow
// with a DSL transform and two destinations (HTTP and file), and proves a
// posted message is persisted, transformed, and delivered to both; a
// filtered message is not delivered; and a failing destination leaves the
// message queued with the error recorded.
func TestPipelineEndToEnd(t *testing.T) {
	var (
		mu       sync.Mutex
		received []string
		keys     []string
		fail     bool
	)
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		received = append(received, string(body))
		keys = append(keys, r.Header.Get("Idempotency-Key"))
	}))
	defer downstream.Close()

	outDir := filepath.Join(t.TempDir(), "out")
	addr := freeAddr(t)
	base := "http://" + addr
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, base+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: base}
	admin := basic(bootstrapAdmin, testAdminPassword)

	flow := `{
	  "id": "adt", "name": "ADT normalize", "sourceType": "http",
	  "transform": {"kind": "Transform", "name": "normalize", "inputs": ["message"], "steps": [
	    {"map": {"from": "PID.5.1", "to": "patient.lastName", "type": "string"}},
	    {"filter": {"when": "patient.lastName == ''", "action": "reject"}},
	    {"set": {"field": "patient.label", "expr": "Patient {{patient.lastName}}"}}
	  ]},
	  "destinations": [
	    {"name": "ehr", "type": "http", "url": "` + downstream.URL + `"},
	    {"name": "archive", "type": "file", "dir": "` + outDir + `"}
	  ]
	}`
	createFlow(t, c, flow)
	if status, body, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"bad","transform":{"name":"t","steps":[{"map":{"from":"a..b","to":"c"}}]}}`, admin); status != http.StatusBadRequest || !strings.Contains(body, `invalid path \"a..b\"`) {
		t.Errorf("invalid flow: %d %q, want 400", status, body)
	}

	post := func(body string) (int, map[string]string) {
		t.Helper()
		status, resp, _ := c.do(http.MethodPost, "/api/v1/flows/adt/messages", body, admin)
		var out map[string]string
		_ = json.Unmarshal([]byte(resp), &out)
		return status, out
	}

	// 1. Delivered and transformed.
	status, res := post(`{"PID":{"5":{"1":"Doe"}}}`)
	if status != http.StatusAccepted || res["status"] != "sent" {
		t.Fatalf("post: %d %v, want 202 sent", status, res)
	}
	want := `{"PID":{"5":{"1":"Doe"}},"patient":{"label":"Patient Doe","lastName":"Doe"}}`
	mu.Lock()
	if len(received) != 1 || received[0] != want || len(keys[0]) != 64 {
		t.Errorf("http destination got %v (keys %v), want %s with an idempotency key", received, keys, want)
	}
	mu.Unlock()
	if file, err := os.ReadFile(filepath.Join(outDir, res["id"])); err != nil || string(file) != want {
		t.Errorf("file destination = %q, %v; want %s", file, err, want)
	}

	// 2. Filtered: not delivered.
	if _, res := post(`{"PID":{"5":{}}}`); res["status"] != "filtered" {
		t.Errorf("filtered message status = %v", res)
	}

	// 3. Failing destination: queued, error recorded, other destination delivered.
	mu.Lock()
	fail = true
	mu.Unlock()
	if _, res := post(`{"PID":{"5":{"1":"Roe"}}}`); res["status"] != "queued" {
		t.Errorf("failing destination status = %v, want queued", res)
	}

	// 4. Not a JSON object.
	if status, _ := post(`not json`); status != http.StatusBadRequest {
		t.Errorf("non-JSON body: %d, want 400", status)
	}
	if status, _, _ := c.do(http.MethodPost, "/api/v1/flows/nope/messages", `{}`, admin); status != http.StatusNotFound {
		t.Errorf("unknown flow: %d, want 404", status)
	}

	// 5. A stopped flow rejects messages; a flow with "transform": null passes
	// raw bodies through.
	createFlow(t, c, `{"id":"stopped"}`)
	c.do(http.MethodPost, "/api/v1/flows/stopped/stop", "", admin)
	if status, body, _ := c.do(http.MethodPost, "/api/v1/flows/stopped/messages", `{}`, admin); status != http.StatusConflict || !strings.Contains(body, "is stopped") {
		t.Errorf("stopped flow: %d %q, want 409", status, body)
	}
	createFlow(t, c, `{"id":"raw","transform":null,"destinations":[{"name":"archive","type":"file","dir":"`+outDir+`"}]}`)
	status, raw, _ := c.do(http.MethodPost, "/api/v1/flows/raw/messages", "MSH|^~\\&|RAW", admin)
	var rawRes map[string]string
	_ = json.Unmarshal([]byte(raw), &rawRes)
	if file, err := os.ReadFile(filepath.Join(outDir, rawRes["id"])); status != http.StatusAccepted || err != nil || string(file) != "MSH|^~\\&|RAW" {
		t.Errorf("passthrough: %d %q file=%q err=%v", status, raw, file, err)
	}
	if status, body, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"ftp","destinations":[{"name":"x","type":"http","url":"ftp://x"}]}`, admin); status != http.StatusBadRequest {
		t.Errorf("ftp destination: %d %q, want 400", status, body)
	}

	// Persistence: every processed message is searchable with its final status.
	for _, s := range []string{"sent", "filtered", "queued"} {
		_, body, _ := c.do(http.MethodGet, "/api/v1/messages?flowId=adt&status="+s, "", admin)
		var msgs []map[string]any
		if err := json.Unmarshal([]byte(body), &msgs); err != nil || len(msgs) != 1 {
			t.Errorf("messages with status %s = %s", s, body)
		}
	}
	mu.Lock()
	if len(received) != 1 {
		t.Errorf("http destination received %d messages, want 1", len(received))
	}
	mu.Unlock()
}
