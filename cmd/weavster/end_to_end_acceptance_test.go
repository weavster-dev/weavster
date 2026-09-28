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
	"sync/atomic"
	"testing"
	"time"
)

// TestEndToEnd: files dropped into a directory are read by a flow's file
// source, transformed, filtered, and delivered to an HTTP and a file
// destination; every message is stored with its final status; the flow's
// statistics, events, and topology show the traffic; and after a restart
// on the same data the messages are still there and a delivery that was
// failing completes.
func TestEndToEnd(t *testing.T) {
	var (
		mu       sync.Mutex
		received []string
		down     atomic.Bool
	)
	ehr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		received = append(received, string(b))
		mu.Unlock()
	}))
	defer ehr.Close()
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(received)
	}
	data, in, archive := t.TempDir(), t.TempDir(), t.TempDir()
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+data+"\"}\n"+
		"delivery: {maxAttempts: 100, backoffBaseMs: 10, retryIntervalMs: 50}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	createFlow(t, c, `{"id":"adt","name":"ADT inbound","source":{"type":"file","dir":"`+in+`","pattern":"*.json","pollIntervalMs":100},`+
		`"transform":{"steps":[{"map":{"from":"patient.last","to":"name"}},{"filter":{"when":"name != ''","action":"accept"}}]},`+
		`"destinations":[{"name":"ehr","type":"http","url":"`+ehr.URL+`"},{"name":"archive","type":"file","dir":"`+archive+`"}]}`)

	drop := func(name, body string) {
		t.Helper()
		p := filepath.Join(in, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-time.Minute)
		_ = os.Chtimes(p, old, old)
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for !ok() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	get := func(path string) string {
		t.Helper()
		code, body, _ := c.do(http.MethodGet, path, "", admin)
		if code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, code, body)
		}
		return body
	}
	byStatus := func(status string) int {
		var msgs []json.RawMessage
		_ = json.Unmarshal([]byte(get("/api/v1/messages?flowId=adt&status="+status)), &msgs)
		return len(msgs)
	}

	drop("a.json", `{"patient":{"last":"Doe"}}`)
	drop("b.json", `{"patient":{}}`) // filtered
	waitFor("two messages", func() bool { return byStatus("sent") == 1 && byStatus("filtered") == 1 })
	if entries, _ := os.ReadDir(archive); count() != 1 || len(entries) != 1 {
		t.Errorf("delivered %d to the EHR and %d files, want 1 each", count(), len(entries))
	}
	mu.Lock()
	if !strings.Contains(received[0], `"name":"Doe"`) {
		t.Errorf("the EHR got %s", received[0])
	}
	mu.Unlock()

	// Statistics, events, and topology show the traffic.
	var stats struct {
		Received, Filtered, Sent int
		Destinations             map[string]struct{ Sent int }
	}
	waitFor("statistics", func() bool {
		_ = json.Unmarshal([]byte(get("/api/v1/flows/adt/stats")), &stats)
		return stats.Received == 2 && stats.Filtered == 1
	})
	if stats.Sent != 1 || stats.Destinations["ehr"].Sent != 1 || stats.Destinations["archive"].Sent != 1 {
		t.Errorf("stats = %+v", stats)
	}
	if events := get("/api/v1/events?flowId=adt"); !strings.Contains(events, `"message.sent"`) || !strings.Contains(events, `"message.filtered"`) {
		t.Errorf("events = %s", events)
	}
	var topo struct {
		Nodes []struct {
			ID       string
			Status   string
			Activity struct{ Received, Sent int }
		}
	}
	_ = json.Unmarshal([]byte(get("/api/v1/topology")), &topo)
	found := false
	for _, n := range topo.Nodes {
		if n.ID == "flow:adt" {
			found = n.Status == "started" && n.Activity.Received == 2 && n.Activity.Sent == 1
		}
	}
	if !found {
		t.Errorf("topology = %+v", topo)
	}

	// The EHR goes down: the next message is stored, archived, and queued
	// for the EHR; the server restarts; the EHR comes back and gets it.
	down.Store(true)
	drop("c.json", `{"patient":{"last":"Roe"}}`)
	waitFor("a queued message", func() bool { return byStatus("queued") == 1 })
	stop()
	down.Store(false)
	stop = startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	waitFor("the queued message sent after the restart", func() bool { return byStatus("sent") == 2 && byStatus("queued") == 0 })
	if byStatus("filtered") != 1 || count() != 2 {
		t.Errorf("after the restart: %d filtered, %d delivered to the EHR", byStatus("filtered"), count())
	}
	if entries, _ := os.ReadDir(archive); len(entries) != 2 {
		t.Errorf("%d archived files, want 2 (the archive is not written again)", len(entries))
	}
}
