package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// flakyDownstream is an HTTP destination whose availability can be toggled.
func flakyDownstream(t *testing.T) (url string, up *atomic.Bool, hits *atomic.Int64) {
	t.Helper()
	up, hits = &atomic.Bool{}, &atomic.Int64{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if !up.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		hits.Add(1)
	}))
	t.Cleanup(ts.Close)
	return ts.URL, up, hits
}

// waitStatus polls the message search until the message reaches status.
func waitStatus(t *testing.T, c apiClient, id, status string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, body, _ := c.do(http.MethodGet, "/api/v1/messages?status="+status, "", basic(bootstrapAdmin, testAdminPassword))
		var msgs []map[string]any
		_ = json.Unmarshal([]byte(body), &msgs)
		for _, m := range msgs {
			if m["id"] == id {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("message %s never reached status %s", id, status)
}

func sendMessage(t *testing.T, c apiClient, flowID, body string) (id, status string) {
	t.Helper()
	code, resp, _ := c.do(http.MethodPost, "/api/v1/flows/"+flowID+"/messages", body, basic(bootstrapAdmin, testAdminPassword))
	var out map[string]string
	if err := json.Unmarshal([]byte(resp), &out); err != nil || code != http.StatusAccepted {
		t.Fatalf("send: %d %q", code, resp)
	}
	return out["id"], out["status"]
}

func createFlow(t *testing.T, c apiClient, flow string) {
	t.Helper()
	if status, body, _ := c.do(http.MethodPost, "/api/v1/flows", flow, basic(bootstrapAdmin, testAdminPassword)); status != http.StatusCreated {
		t.Fatalf("create flow: %d %q", status, body)
	}
}

// TestRetryRecoversQueuedMessage: a queued message is delivered once the
// destination recovers, by the background retry worker.
func TestRetryRecoversQueuedMessage(t *testing.T) {
	url, up, hits := flakyDownstream(t)
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n"+
		"delivery: {maxAttempts: 50, backoffBaseMs: 10, retryIntervalMs: 20}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	createFlow(t, c, `{"id":"f","destinations":[{"name":"ehr","type":"http","url":"`+url+`"}]}`)

	id, status := sendMessage(t, c, "f", "x")
	if status != "queued" {
		t.Fatalf("status = %s, want queued", status)
	}
	up.Store(true)
	waitStatus(t, c, id, "sent")
	if hits.Load() != 1 {
		t.Errorf("downstream received %d deliveries, want 1", hits.Load())
	}
}

// TestQueuedWorkSurvivesRestart: a message queued when the server stops is
// delivered after the server restarts.
func TestQueuedWorkSurvivesRestart(t *testing.T) {
	url, up, hits := flakyDownstream(t)
	addr := freeAddr(t)
	// A long retry interval: after the startup pass, the first server never
	// retries, so only the restarted server can deliver the message.
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n"+
		"delivery: {maxAttempts: 5, backoffBaseMs: 10, retryIntervalMs: 600000}\n")
	args := []string{"server", "--config", cfg}
	c := apiClient{t: t, base: "http://" + addr}

	stop := startCLI(t, args, "http://"+addr+"/api/openapi.yaml")
	createFlow(t, c, `{"id":"f","destinations":[{"name":"ehr","type":"http","url":"`+url+`"}]}`)
	id, status := sendMessage(t, c, "f", "x")
	if status != "queued" {
		t.Fatalf("status = %s, want queued", status)
	}
	stop()

	up.Store(true)
	stop = startCLI(t, args, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	waitStatus(t, c, id, "sent")
	if hits.Load() != 1 {
		t.Errorf("downstream received %d deliveries, want 1", hits.Load())
	}
}

// TestDeadLetterAfterMaxAttempts: a destination that never recovers moves
// the message to dead-lettered after delivery.maxAttempts.
func TestDeadLetterAfterMaxAttempts(t *testing.T) {
	url, _, _ := flakyDownstream(t)
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n"+
		"delivery: {maxAttempts: 3, backoffBaseMs: 10, retryIntervalMs: 20}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	createFlow(t, c, `{"id":"f","destinations":[{"name":"ehr","type":"http","url":"`+url+`"}]}`)

	id, _ := sendMessage(t, c, "f", "x")
	waitStatus(t, c, id, "dead-lettered")
	_, body, _ := c.do(http.MethodGet, "/api/v1/events?type=message.dead-lettered", "", basic(bootstrapAdmin, testAdminPassword))
	if !strings.Contains(body, `"type":"message.dead-lettered"`) || !strings.Contains(body, id) {
		t.Errorf("dead-letter event missing: %s", body)
	}
}
