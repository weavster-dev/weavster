package main

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestHTTPDestinationOptions: an http destination's method, timeout, and
// redirect limit apply to real deliveries; a redirect is not followed
// unless allowed, and a 302 never is (it would turn the POST into a GET).
func TestHTTPDestinationOptions(t *testing.T) {
	var mu sync.Mutex
	var got []string
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, r.Method+" "+r.URL.Path+" "+string(b))
		mu.Unlock()
		switch r.URL.Path {
		case "/moved":
			http.Redirect(w, r, "/in", http.StatusTemporaryRedirect)
		case "/found":
			http.Redirect(w, r, "/in", http.StatusFound)
		case "/slow":
			time.Sleep(1500 * time.Millisecond)
		}
	}))
	defer downstream.Close()
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n"+
		"delivery: {retryIntervalMs: 600000}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}

	for _, tt := range []struct {
		name, dest, status, requests string
	}{
		{"put", `"url":"` + downstream.URL + `/in","method":"PUT"`, "sent", "PUT /in m"},
		{"redirect refused by default", `"url":"` + downstream.URL + `/moved"`, "queued", "POST /moved m"},
		{"307 followed", `"url":"` + downstream.URL + `/moved","maxRedirects":1`, "sent", "POST /moved m|POST /in m"},
		{"302 never followed", `"url":"` + downstream.URL + `/found","maxRedirects":3`, "queued", "POST /found m"},
		{"timeout", `"url":"` + downstream.URL + `/slow","timeoutMs":1000`, "queued", "POST /slow m"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mu.Lock()
			got = nil
			mu.Unlock()
			id := strings.ReplaceAll(tt.name, " ", "-")
			createFlow(t, c, `{"id":"`+id+`","destinations":[{"name":"out","type":"http",`+tt.dest+`}]}`)
			if _, status := sendMessage(t, c, id, "m"); status != tt.status {
				t.Errorf("status = %s, want %s", status, tt.status)
			}
			mu.Lock()
			defer mu.Unlock()
			if strings.Join(got, "|") != tt.requests {
				t.Errorf("requests = %q, want %q", strings.Join(got, "|"), tt.requests)
			}
		})
	}

	admin := basic(bootstrapAdmin, testAdminPassword)
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"bad","destinations":[{"name":"out","type":"file","dir":"/tmp/x","timeoutMs":5000}]}`, admin); code != http.StatusBadRequest {
		t.Errorf("timeoutMs on a file destination: %d %s", code, body)
	}
}

// TestHTTPSourceReadTimeout: a request whose body is not sent within the
// source's readTimeoutMs is cut off.
func TestHTTPSourceReadTimeout(t *testing.T) {
	addr, src := freeAddr(t), freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	createFlow(t, c, `{"id":"slow","source":{"type":"http","address":"`+src+`","readTimeoutMs":1000}}`)
	var conn net.Conn
	deadline := time.Now().Add(10 * time.Second)
	for {
		var err error
		if conn, err = net.DialTimeout("tcp", src, 200*time.Millisecond); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the source never listened")
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer func() { _ = conn.Close() }()
	// Promise a body, then stall.
	if _, err := io.WriteString(conn, "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 10\r\n\r\n{"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, _ = io.ReadAll(conn) // returns when the server closes the connection
	if waited := time.Since(start); waited < 500*time.Millisecond || waited > 5*time.Second {
		t.Errorf("connection closed after %v, want about 1s", waited)
	}
}
