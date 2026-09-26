package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/gateway"
)

// TestGracefulShutdownRequeuesInFlightWork sends SIGTERM while a delivery is
// hanging: the server exits within listen.shutdownTimeoutMs, and after a
// restart the stored message is delivered with the same idempotency key.
func TestGracefulShutdownRequeuesInFlightWork(t *testing.T) {
	var (
		mu      sync.Mutex
		keys    []string
		hanging = true
	)
	arrived := make(chan struct{}, 1)
	release := make(chan struct{})
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		hang := hanging
		mu.Unlock()
		if hang {
			select {
			case arrived <- struct{}{}:
			default:
			}
			<-release
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer downstream.Close()
	defer close(release)

	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\", shutdownTimeoutMs: 200}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n"+
		"delivery: {backoffBaseMs: 10, retryIntervalMs: 600000}\n")
	args := []string{"server", "--config", cfg}
	c := apiClient{t: t, base: "http://" + addr}

	stop, _ := startCLIWithStderr(t, args, "http://"+addr+"/api/openapi.yaml")
	createFlow(t, c, `{"id":"f","destinations":[{"name":"ehr","type":"http","url":"`+downstream.URL+`"}]}`)

	// Send the message from a plain goroutine (no t calls): the old server
	// abandons this request at the shutdown deadline.
	go func() {
		req, _ := http.NewRequest(http.MethodPost, c.base+"/api/v1/flows/f/messages", strings.NewReader("x"))
		req.Header.Set(gateway.MarkerHeader, gateway.MarkerValue)
		req.SetBasicAuth(bootstrapAdmin, testAdminPassword)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}()
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("the delivery never reached the downstream")
	}

	start := time.Now()
	stop() // SIGTERM; asserts a clean exit
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("shutdown took %v; want it bounded by the 200ms deadline", elapsed)
	}

	mu.Lock()
	hanging = false
	mu.Unlock()
	time.Sleep(50 * time.Millisecond) // let the 10ms backoff elapse, if an attempt was recorded
	stop = startCLI(t, args, "http://"+addr+"/api/openapi.yaml")
	defer stop()

	_, body, _ := c.do(http.MethodGet, "/api/v1/messages?flowId=f", "", basic(bootstrapAdmin, testAdminPassword))
	var msgs []struct{ ID string }
	if err := json.Unmarshal([]byte(body), &msgs); err != nil || len(msgs) != 1 {
		t.Fatalf("message was not stored before shutdown: %s", body)
	}
	waitStatus(t, c, msgs[0].ID, "sent")
	mu.Lock()
	defer mu.Unlock()
	if len(keys) < 2 || keys[0] == "" {
		t.Fatalf("deliveries = %v, want the hung attempt and the resumed one", keys)
	}
	for _, k := range keys {
		if k != keys[0] {
			t.Errorf("idempotency keys differ across attempts: %v", keys)
		}
	}
}

// TestShutdownIsBounded: a hung request or a busy retry worker cannot hold
// shutdown past its deadline.
func TestShutdownIsBounded(t *testing.T) {
	logs := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))

	// A retry worker that never finishes.
	start := time.Now()
	if shutdown(nil, func() {}, make(chan struct{}), 50*time.Millisecond, logger) {
		t.Error("reported a clean shutdown with the worker still running")
	}
	if time.Since(start) > time.Second || !strings.Contains(logs.String(), "retry worker still delivering") {
		t.Errorf("worker wait not bounded or not logged: %v %q", time.Since(start), logs.String())
	}

	// A request that never finishes: the listener is closed at the deadline.
	hung, inHandler := make(chan struct{}), make(chan struct{})
	defer close(hung)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(inHandler)
		<-hung
	})}
	go func() { _ = srv.Serve(ln) }()
	go func() {
		if resp, err := http.Get("http://" + ln.Addr().String()); err == nil {
			_ = resp.Body.Close()
		}
	}()
	select {
	case <-inHandler:
	case <-time.After(5 * time.Second):
		t.Fatal("request never reached the handler")
	}
	done := make(chan struct{})
	close(done)
	start = time.Now()
	if shutdown([]*http.Server{srv}, func() {}, done, 50*time.Millisecond, logger) {
		t.Error("reported a clean shutdown with a request still running")
	}
	if time.Since(start) > time.Second {
		t.Errorf("request drain not bounded: %v", time.Since(start))
	}

	// Nothing running: clean.
	if !shutdown(nil, func() {}, done, 50*time.Millisecond, logger) {
		t.Error("idle shutdown not reported clean")
	}
}
