package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestDestinationTransforms: each destination gets its own transform of
// the flow output, a destination filter drops the message for that
// destination only, and a held message is transformed the same way after a
// restart.
func TestDestinationTransforms(t *testing.T) {
	type capture struct {
		mu     sync.Mutex
		bodies []string
	}
	listen := func(c *capture) *httptest.Server {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			c.mu.Lock()
			c.bodies = append(c.bodies, r.Header.Get("Content-Type")+" "+string(b))
			c.mu.Unlock()
		}))
		t.Cleanup(s.Close)
		return s
	}
	got := func(c *capture) []string {
		c.mu.Lock()
		defer c.mu.Unlock()
		return append([]string(nil), c.bodies...)
	}
	var ca, cb capture
	sa, sb := listen(&ca), listen(&cb)
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\n"+storeConfig(t)+
		"delivery: {backoffBaseMs: 10, retryIntervalMs: 20}\n")
	args := []string{"server", "--config", cfg}
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	stop := startCLI(t, args, c.base+"/api/openapi.yaml")

	bad := `{"id":"x","destinations":[{"name":"a","type":"http","url":"` + sa.URL + `","transform":{"steps":[{"filter":{"when":"k","action":"drop"}}]}}]}`
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows", bad, admin); code != http.StatusBadRequest {
		t.Errorf("invalid destination transform: %d %q, want 400", code, body)
	}
	createFlow(t, c, `{"id":"f","transform":{"steps":[{"map":{"from":"PID.5","to":"name"}}]},"destinations":[`+
		`{"name":"a","type":"http","url":"`+sa.URL+`","transform":{"steps":[{"filter":{"when":"kind == 'orm'","action":"reject"}}]}},`+
		`{"name":"b","type":"http","url":"`+sb.URL+`","transform":{"steps":[{"set":{"field":"label","expr":"B {{name}}"}}]}}]}`)

	if _, status := sendMessage(t, c, "f", `{"kind":"adt","PID":{"5":"Doe"}}`); status != "sent" {
		t.Fatalf("adt: %s", status)
	}
	if _, status := sendMessage(t, c, "f", `{"kind":"orm","PID":{"5":"Roe"}}`); status != "sent" {
		t.Fatalf("orm: %s", status)
	}
	wantA := []string{`application/json {"PID":{"5":"Doe"},"kind":"adt","name":"Doe"}`}
	wantB := []string{
		`application/json {"PID":{"5":"Doe"},"kind":"adt","label":"B Doe","name":"Doe"}`,
		`application/json {"PID":{"5":"Roe"},"kind":"orm","label":"B Roe","name":"Roe"}`,
	}
	if a, b := got(&ca), got(&cb); strings.Join(a, "\n") != strings.Join(wantA, "\n") || strings.Join(b, "\n") != strings.Join(wantB, "\n") {
		t.Fatalf("a got %q\nb got %q", a, b)
	}

	// Hold a message for b, restart, then release it: b's transform runs
	// on the stored flow output.
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows/f/destinations/b/stop", "", admin); code != http.StatusOK {
		t.Fatalf("stop b: %d %q", code, body)
	}
	id, _ := sendMessage(t, c, "f", `{"kind":"adt","PID":{"5":"Poe"}}`)
	stop()
	if !restartable(t) {
		return
	}
	stop = startCLI(t, args, c.base+"/api/openapi.yaml")
	defer stop()
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows/f/destinations/b/start", "", admin); code != http.StatusOK {
		t.Fatalf("start b: %d %q", code, body)
	}
	waitStatus(t, c, id, "sent")
	deadline := time.Now().Add(5 * time.Second)
	for len(got(&cb)) < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if b := got(&cb); len(b) != 3 || b[2] != `application/json {"PID":{"5":"Poe"},"kind":"adt","label":"B Poe","name":"Poe"}` {
		t.Errorf("after restart b got %q", b)
	}
}
