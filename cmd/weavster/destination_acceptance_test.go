package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestDestinationStartStop: a stopped destination holds its messages
// (queued) while the other destination keeps receiving; starting it
// delivers the held messages. The stopped set survives a restart.
func TestDestinationStartStop(t *testing.T) {
	var a, b atomic.Int64
	counter := func(n *atomic.Int64) *httptest.Server {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			n.Add(1)
		}))
		t.Cleanup(s.Close)
		return s
	}
	sa, sb := counter(&a), counter(&b)
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\n"+storeConfig(t)+
		"delivery: {backoffBaseMs: 10, retryIntervalMs: 20}\n")
	args := []string{"server", "--config", cfg}
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	stop := startCLI(t, args, c.base+"/api/openapi.yaml")

	createFlow(t, c, `{"id":"f","destinations":[{"name":"a","type":"http","url":"`+sa.URL+`"},{"name":"b","type":"http","url":"`+sb.URL+`"}]}`)
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows/f/destinations/b/stop", "", admin); code != http.StatusOK || !strings.Contains(body, `"stoppedDestinations":["b"]`) {
		t.Fatalf("stop b: %d %q", code, body)
	}
	for _, path := range []string{"/api/v1/flows/f/destinations/zz/stop", "/api/v1/flows/nope/destinations/b/stop", "/api/v1/flows/f/destinations/b/explode"} {
		if code, _, _ := c.do(http.MethodPost, path, "", admin); code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", path, code)
		}
	}
	id1, status := sendMessage(t, c, "f", "one")
	id2, _ := sendMessage(t, c, "f", "two")
	if status != "queued" || a.Load() != 2 || b.Load() != 0 {
		t.Fatalf("while b stopped: status %s, a=%d b=%d", status, a.Load(), b.Load())
	}
	stop()
	if !restartable(t) {
		return
	}

	stop, stderr := startCLIWithStderr(t, args, c.base+"/api/openapi.yaml")
	defer stop()
	defer func() {
		if t.Failed() {
			t.Logf("server stderr:\n%s", stderr.String())
		}
	}()
	if _, body, _ := c.do(http.MethodGet, "/api/v1/flows/f", "", admin); !strings.Contains(body, `"stoppedDestinations":["b"]`) {
		t.Errorf("stopped set lost on restart: %s", body)
	}
	// Status updates must not clear it.
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows/f/pause", "", admin); code != http.StatusOK || !strings.Contains(body, `"stoppedDestinations":["b"]`) {
		t.Errorf("pause dropped stoppedDestinations: %d %q", code, body)
	}
	c.do(http.MethodPost, "/api/v1/flows/f/resume", "", admin)
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows/f/destinations/b/start", "", admin); code != http.StatusOK || strings.Contains(body, "stoppedDestinations") {
		t.Fatalf("start b: %d %q", code, body)
	}
	waitStatus(t, c, id1, "sent")
	waitStatus(t, c, id2, "sent")
	if b.Load() != 2 || a.Load() != 2 {
		t.Errorf("after start: a=%d b=%d, want 2 and 2", a.Load(), b.Load())
	}
	if _, body, _ := c.do(http.MethodGet, "/api/v1/flows/export?ids=f", "", admin); strings.Contains(body, "stoppedDestinations") {
		t.Errorf("export carries runtime state: %s", body)
	}
}
