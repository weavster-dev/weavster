package main

import (
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestHTTPSource: a started flow with an http source listens on its own
// address and runs each request with its method and path through the flow;
// stopping the flow or the server closes the port; ports-in-use lists it;
// a port has one flow source, and a port that cannot be opened is reported.
func TestHTTPSource(t *testing.T) {
	addr, src := freeAddr(t), freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	stopped := false
	defer func() {
		if !stopped {
			stop()
		}
	}()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	out := t.TempDir()
	send := func(method, path, body string) (int, string, http.Header) {
		t.Helper()
		req, err := http.NewRequest(method, "http://"+src+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, err.Error(), nil
		}
		defer func() { _ = res.Body.Close() }()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b), res.Header
	}
	listening := func() bool {
		conn, err := net.DialTimeout("tcp", src, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
		}
		return err == nil
	}
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	createFlow(t, c, `{"id":"adt","source":{"type":"http","address":"`+src+`","path":"/adt"},`+
		`"destinations":[{"name":"out","type":"file","dir":"`+out+`"}]}`)
	waitFor("the source to listen", listening)
	if code, body, _ := send(http.MethodPost, "/adt", `{"k":"a"}`); code != http.StatusAccepted || !strings.Contains(body, `"id":`) {
		t.Fatalf("POST /adt: %d %s", code, body)
	}
	if entries, _ := os.ReadDir(out); len(entries) != 1 {
		t.Errorf("delivered %d files, want 1", len(entries))
	}
	if _, body, _ := c.do(http.MethodGet, "/api/v1/messages?flowId=adt", "", admin); !strings.Contains(body, `"source.http.path":"/adt"`) {
		t.Errorf("messages = %s", body)
	}
	if code, body, _ := send(http.MethodPost, "/other", `{}`); code != http.StatusNotFound {
		t.Errorf("other path: %d %s", code, body)
	}
	if code, _, h := send(http.MethodGet, "/adt", ""); code != http.StatusMethodNotAllowed || h.Get("Allow") != http.MethodPost {
		t.Errorf("GET: %d Allow=%q", code, h.Get("Allow"))
	}
	if _, body, _ := c.do(http.MethodGet, "/api/v1/flows/ports-in-use", "", admin); !strings.Contains(body, `"usedBy":"flow:adt"`) || !strings.Contains(body, `"usedBy":"api"`) {
		t.Errorf("ports-in-use = %s", body)
	}

	// Stopping the flow closes the port; starting it opens it again.
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows/adt/stop", "", admin); code != http.StatusOK {
		t.Fatalf("stop: %d %s", code, body)
	}
	waitFor("the port to close", func() bool { return !listening() })
	if _, body, _ := c.do(http.MethodGet, "/api/v1/flows/ports-in-use", "", admin); strings.Contains(body, "flow:adt") {
		t.Errorf("ports-in-use after stop = %s", body)
	}
	c.do(http.MethodPost, "/api/v1/flows/adt/start", "", admin)
	waitFor("the port to open again", listening)

	// One port, one flow source; addresses need a port.
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"twin","source":{"type":"http","address":"`+src+`"}}`, admin); code != http.StatusBadRequest || !strings.Contains(body, "a port can have one flow source") {
		t.Errorf("second flow on the same port: %d %s", code, body)
	}
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"noport","source":{"type":"http","address":"127.0.0.1"}}`, admin); code != http.StatusBadRequest || !strings.Contains(body, "source.address must be host:port") {
		t.Errorf("address without a port: %d %s", code, body)
	}

	// A port something else holds (here, the API) is reported as an event.
	createFlow(t, c, `{"id":"busy","source":{"type":"http","address":"`+addr+`"}}`)
	waitFor("a source.http.failed event", func() bool {
		_, body, _ := c.do(http.MethodGet, "/api/v1/events?type=source.http.failed", "", admin)
		return strings.Contains(body, `"flowId":"busy"`) && strings.Contains(body, addr)
	})

	// Server shutdown closes the flow's port.
	stop()
	stopped = true
	if listening() {
		t.Error("the source still listens after the server stopped")
	}
}
