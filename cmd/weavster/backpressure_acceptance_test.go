package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestBackpressure: with processing.maxConcurrent 1, a message arriving
// while another is processed waits processing.waitMs, then is refused with
// 503 and Retry-After; once the first is done it is accepted. A flow
// destination hands its message on inside the sender's slot, so a chain of
// flows still works with a single slot.
func TestBackpressure(t *testing.T) {
	release, arrived := make(chan struct{}), make(chan struct{}, 1)
	slow := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		select {
		case arrived <- struct{}{}: // the first message holds its slot now
		default:
		}
		<-release
	}))
	defer slow.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n"+
		"processing: {maxConcurrent: 1, waitMs: 200}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	out := t.TempDir()
	createFlow(t, c, `{"id":"slow","destinations":[{"name":"ehr","type":"http","url":"`+slow.URL+`","timeoutMs":30000}]}`)
	createFlow(t, c, `{"id":"store","destinations":[{"name":"out","type":"file","dir":"`+out+`"}]}`)
	createFlow(t, c, `{"id":"intake","destinations":[{"name":"next","type":"flow","flow":"store"}]}`)

	first := make(chan int, 1)
	go func() { // not c.do: its t.Fatal must not run outside the test goroutine
		req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/api/v1/flows/slow/messages", strings.NewReader(`{"n":1}`))
		admin(req)
		req.Header.Set("X-Weavster-CSRF", "1")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			first <- 0
			return
		}
		_ = resp.Body.Close()
		first <- resp.StatusCode
	}()
	// Wait until the first message holds the slot: its delivery reached
	// the slow destination.
	select {
	case <-arrived:
	case code := <-first:
		t.Fatalf("the first message finished early: %d", code)
	case <-time.After(10 * time.Second):
		t.Fatal("the first message never reached its destination")
	}
	code, body, header := c.do(http.MethodPost, "/api/v1/flows/store/messages", `{"n":2}`, admin)
	if code != http.StatusServiceUnavailable || header.Get("Retry-After") != "1" || !strings.Contains(body, "the server is busy") {
		t.Fatalf("second message while busy: %d %v %s", code, header, body)
	}
	close(release)
	if code := <-first; code != http.StatusAccepted {
		t.Errorf("first message: %d", code)
	}
	if code, resp, _ := c.do(http.MethodPost, "/api/v1/flows/store/messages", `{"n":3}`, admin); code != http.StatusAccepted {
		t.Errorf("after the first finished: %d %s", code, resp)
	}

	// A chain of flows with one slot.
	before, _ := os.ReadDir(out)
	if code, resp, _ := c.do(http.MethodPost, "/api/v1/flows/intake/messages", `{"n":4}`, admin); code != http.StatusAccepted || !strings.Contains(resp, `"status":"sent"`) {
		t.Errorf("chained flows with one slot: %d %s", code, resp)
	}
	if after, _ := os.ReadDir(out); len(after) != len(before)+1 {
		t.Errorf("the chained message did not reach the target flow: %d files, want %d", len(after), len(before)+1)
	}
}
