package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestStatsSeries: the running server samples every flow's lifetime
// statistics at stats.sampleIntervalMs; the series shows the counters rise
// as messages are processed, filtered by flow and time.
func TestStatsSeries(t *testing.T) {
	cfg := serverconfig.Default()
	cfg.Stats.SampleIntervalMs = 100
	c := startWithWorkers(t, cfg)
	admin := basic(bootstrapAdmin, testAdminPassword)

	createFlow(t, c, `{"id":"adt","destinations":[{"name":"out","type":"file","dir":"`+t.TempDir()+`"}]}`)
	createFlow(t, c, `{"id":"lab","destinations":[{"name":"out","type":"file","dir":"`+t.TempDir()+`"}]}`)
	start := time.Now().UTC()
	sendMessage(t, c, "adt", `{"k":"v"}`)
	sendMessage(t, c, "adt", `{"k":"w"}`)

	type sample struct {
		At     time.Time
		FlowID string
		Stats  struct{ Received, Sent int64 }
	}
	series := func(q string) (samples []sample) {
		t.Helper()
		code, body, _ := c.do(http.MethodGet, "/api/v1/stats/series"+q, "", admin)
		if err := json.Unmarshal([]byte(body), &samples); code != http.StatusOK || err != nil {
			t.Fatalf("series %s: %d %s", q, code, body)
		}
		return samples
	}
	// Wait until a sample shows both messages sent.
	deadline := time.Now().Add(10 * time.Second)
	for {
		s := series("?flowId=adt")
		if n := len(s); n > 0 && s[n-1].Stats.Received == 2 && s[n-1].Stats.Sent == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no sample with both messages: %+v", s)
		}
		time.Sleep(50 * time.Millisecond)
	}

	all := series("")
	flows := map[string]bool{}
	for i, s := range all {
		flows[s.FlowID] = true
		if i > 0 && s.At.Before(all[i-1].At) {
			t.Errorf("not oldest first: %+v", all)
		}
	}
	if !flows["adt"] || !flows["lab"] {
		t.Errorf("every flow is sampled: %+v", flows)
	}
	for _, s := range series("?flowId=lab") {
		if s.FlowID != "lab" || s.Stats.Received != 0 {
			t.Errorf("lab sample = %+v", s)
		}
	}
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if s := series("?from=" + future); len(s) != 0 {
		t.Errorf("from the future = %+v", s)
	}
	if s := series("?to=" + start.Add(-time.Hour).Format(time.RFC3339)); len(s) != 0 {
		t.Errorf("before the start = %+v", s)
	}
	if s := series("?limit=1"); len(s) != 1 || s[0].FlowID != "lab" {
		t.Errorf("limit=1 keeps the newest sample (flows are sampled in id order) = %+v", s)
	}
	// A deleted flow's samples go with it; a new flow with the same id
	// starts a new series.
	if code, body, _ := c.do(http.MethodDelete, "/api/v1/flows/lab", "", admin); code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", code, body)
	}
	for _, s := range series("") {
		if s.FlowID == "lab" {
			t.Errorf("deleted flow still listed: %+v", s)
		}
	}
	recreated := time.Now().UTC()
	createFlow(t, c, `{"id":"lab","destinations":[{"name":"out","type":"file","dir":"`+t.TempDir()+`"}]}`)
	for _, s := range series("?flowId=lab") {
		if s.At.Before(recreated) {
			t.Errorf("old sample of a recreated flow: %+v", s)
		}
	}
	for _, tt := range []struct {
		q, user string
		status  int
		want    string
	}{
		{"?flowId=nope", "", http.StatusNotFound, "flow not found"},
		{"?from=yesterday", "", http.StatusBadRequest, "RFC 3339"},
		{"?limit=0", "", http.StatusBadRequest, "limit must be between"},
		{"", "ops", http.StatusForbidden, "flows:view"},
	} {
		auth := admin
		if tt.user != "" {
			c.do(http.MethodPost, "/api/v1/users", `{"username":"ops","password":"Ops-Passw0rd-1","permissions":["messages:view"],"mustChangePassword":false}`, admin)
			auth = basic("ops", "Ops-Passw0rd-1")
		}
		if code, body, _ := c.do(http.MethodGet, "/api/v1/stats/series"+tt.q, "", auth); code != tt.status || !strings.Contains(body, tt.want) {
			t.Errorf("%s %s: %d %s", tt.q, tt.user, code, body)
		}
	}
}

// TestStatsSeriesFirstSample: the first sample is taken when the server
// starts, not one interval later.
func TestStatsSeriesFirstSample(t *testing.T) {
	cfg := serverconfig.Default()
	cfg.Store.Dialect, cfg.Store.DSN = serverconfig.DialectPostgres, postgresStoreDSN(t)
	first := startComposed(t, cfg, io.Discard)
	createFlow(t, first, `{"id":"adt","destinations":[{"name":"out","type":"file","dir":"`+t.TempDir()+`"}]}`)

	cfg.Stats.SampleIntervalMs = 3600000 // the next sample is an hour away
	c := startWithWorkers(t, cfg)
	deadline := time.Now().Add(5 * time.Second)
	for {
		code, body, _ := c.do(http.MethodGet, "/api/v1/stats/series?flowId=adt", "", basic(bootstrapAdmin, testAdminPassword))
		if code == http.StatusOK && strings.Contains(body, `"flowId":"adt"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no startup sample: %d %s", code, body)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// startWithWorkers is startComposed that also runs the background workers
// (statistics sampling, delivery retries) until the test ends.
func startWithWorkers(t *testing.T, cfg serverconfig.Config) apiClient {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	handler, closeStore, workers, err := buildServerWithWorkers(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), io.Discard, cfg)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { workers(ctx); close(done) }()
	ts := httptest.NewServer(handler)
	t.Cleanup(func() { cancel(); <-done; ts.Close(); _ = closeStore() })
	return apiClient{t: t, base: ts.URL}
}
