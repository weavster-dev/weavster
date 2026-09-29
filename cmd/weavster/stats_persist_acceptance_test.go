package main

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// TestStatsSurviveRestart: current and lifetime statistics, a reset, and
// the time series are still there after the server restarts; a deleted
// flow's are not, even when a flow of the same id is created again.
func TestStatsSurviveRestart(t *testing.T) {
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstats: {sampleIntervalMs: 100, retentionHours: 1}\n"+storeConfig(t))
	args := []string{"server", "--config", cfg}
	stop := startCLI(t, args, "http://"+addr+"/api/openapi.yaml")
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)

	type counts struct{ Received, Sent int64 }
	stats := func(path string) counts {
		t.Helper()
		var out counts
		code, body, _ := c.do(http.MethodGet, path, "", admin)
		if code != http.StatusOK || json.Unmarshal([]byte(body), &out) != nil {
			t.Fatalf("%s: %d %s", path, code, body)
		}
		return out
	}
	type sample struct {
		At    time.Time
		Stats counts
	}
	series := func(flow string) []sample {
		t.Helper()
		var out []sample
		code, body, _ := c.do(http.MethodGet, "/api/v1/stats/series?flowId="+flow, "", admin)
		if code != http.StatusOK || json.Unmarshal([]byte(body), &out) != nil {
			t.Fatalf("series %s: %d %s", flow, code, body)
		}
		return out
	}

	createFlow(t, c, `{"id":"adt","destinations":[{"name":"out","type":"file","dir":"`+t.TempDir()+`"}]}`)
	createFlow(t, c, `{"id":"lab","destinations":[{"name":"out","type":"file","dir":"`+t.TempDir()+`"}]}`)
	sendMessage(t, c, "adt", `{"k":"v"}`)
	sendMessage(t, c, "adt", `{"k":"w"}`)
	sendMessage(t, c, "lab", `{"k":"x"}`)
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		if s := series("adt"); len(s) > 0 && s[len(s)-1].Stats.Sent == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no sample with both messages")
		}
	}
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows/lab/stats/reset", "", admin); code != http.StatusNoContent {
		t.Fatalf("reset: %d %s", code, body)
	}
	before := len(series("adt"))
	stop()
	if !restartable(t) {
		return
	}

	stop = startCLI(t, args, "http://"+addr+"/api/openapi.yaml")
	for _, tt := range []struct {
		path string
		want counts
	}{
		{"/api/v1/flows/adt/stats", counts{2, 2}},
		{"/api/v1/flows/adt/stats?lifetime=true", counts{2, 2}},
		{"/api/v1/flows/lab/stats", counts{0, 0}},
		{"/api/v1/flows/lab/stats?lifetime=true", counts{1, 1}},
	} {
		if got := stats(tt.path); got != tt.want {
			t.Errorf("%s after a restart = %+v, want %+v", tt.path, got, tt.want)
		}
	}
	if s := series("adt"); len(s) < before || s[before-1].Stats.Sent != 2 {
		t.Errorf("series after a restart has %d samples (%+v), want the %d from before first", len(s), s, before)
	}
	sendMessage(t, c, "adt", `{"k":"y"}`) // counting goes on from the stored totals
	if got := stats("/api/v1/flows/adt/stats?lifetime=true"); got != (counts{3, 3}) {
		t.Errorf("lifetime after one more = %+v", got)
	}

	// A deleted flow's statistics are gone, also from the store.
	if code, body, _ := c.do(http.MethodDelete, "/api/v1/flows/lab", "", admin); code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", code, body)
	}
	stop()
	stop = startCLI(t, args, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	createFlow(t, c, `{"id":"lab","destinations":[{"name":"out","type":"file","dir":"`+t.TempDir()+`"}]}`)
	if got := stats("/api/v1/flows/lab/stats?lifetime=true"); got != (counts{}) {
		t.Errorf("a new lab after a restart = %+v", got)
	}
	for _, s := range series("lab") {
		if s.Stats.Received != 0 {
			t.Errorf("a new lab has an old sample: %+v", s)
		}
	}
}
