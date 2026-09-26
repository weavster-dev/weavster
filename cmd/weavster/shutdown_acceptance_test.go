package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
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

	done := make(chan int, 1)
	errb := &syncBuffer{}
	go func() { done <- run(args, strings.NewReader(""), io.Discard, errb) }()
	waitReady(t, "http://"+addr+"/api/openapi.yaml")
	createFlow(t, c, `{"id":"f","destinations":[{"name":"ehr","type":"http","url":"`+downstream.URL+`"}]}`)

	go func() { // blocks until the server gives up on it
		c.do(http.MethodPost, "/api/v1/flows/f/messages", "x", basic(bootstrapAdmin, testAdminPassword))
	}()
	<-arrived

	start := time.Now()
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit = %d (stderr %q)", code, errb.String())
		}
		if elapsed := time.Since(start); elapsed > 3*time.Second {
			t.Errorf("shutdown took %v; want it bounded by the 200ms deadline", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop while a delivery was hanging")
	}

	mu.Lock()
	hanging = false
	mu.Unlock()
	time.Sleep(50 * time.Millisecond) // let the 10ms backoff elapse, if an attempt was recorded
	stop := startCLI(t, args, "http://"+addr+"/api/openapi.yaml")
	defer stop()

	_, body, _ := c.do(http.MethodGet, "/api/v1/messages?flowId=f", "", basic(bootstrapAdmin, testAdminPassword))
	id := between(body, `"id":"`, `"`)
	if id == "" {
		t.Fatalf("message was not stored before shutdown: %s", body)
	}
	waitStatus(t, c, id, "sent")
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

func between(s, start, end string) string {
	_, rest, ok := strings.Cut(s, start)
	if !ok {
		return ""
	}
	v, _, _ := strings.Cut(rest, end)
	return v
}
