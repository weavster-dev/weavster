package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestAttemptCodes: each destination's attempt record keeps the last
// failure's protocol-specific code and when the attempt ended — an HTTP
// status, an MLLP ACK code, a refused connection, a stopped target flow —
// and a later success clears the code.
func TestAttemptCodes(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	ehr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer ehr.Close()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	closed := ln.Addr().String()
	_ = ln.Close()
	lab := &mllpReceiver{codes: []string{"AE"}}

	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n"+
		"delivery: {maxAttempts: 50, backoffBaseMs: 10, retryIntervalMs: 20}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	createFlow(t, c, `{"id":"target","destinations":[]}`)
	if code, resp, _ := c.do(http.MethodPost, "/api/v1/flows/target/stop", "", admin); code != http.StatusOK {
		t.Fatalf("stop target: %d %s", code, resp)
	}
	createFlow(t, c, `{"id":"out","inputFormat":"hl7v2","destinations":[`+
		`{"name":"ehr","type":"http","url":"`+ehr.URL+`"},`+
		`{"name":"lab","type":"mllp","address":"`+lab.serve(t)+`","timeoutMs":2000},`+
		`{"name":"down","type":"http","url":"http://`+closed+`/in"},`+
		`{"name":"next","type":"flow","flow":"target"}]}`)
	id, _ := sendMessage(t, c, "out", "MSH|^~\\&|W|H|LAB|H|20260927120000||ADT^A01|M1|P|2.5\rPID|1\r")

	attempts := func() map[string]struct {
		Attempts      int
		LastCode      string
		LastAttemptAt *time.Time
	} {
		t.Helper()
		_, body, _ := c.do(http.MethodGet, "/api/v1/messages/"+id, "", admin)
		var m struct {
			Attempts map[string]struct {
				Attempts      int
				LastCode      string
				LastAttemptAt *time.Time
			}
		}
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		return m.Attempts
	}
	want := map[string]string{"ehr": "http:503", "lab": "mllp:AE", "down": "net:refused", "next": "flow:not-running"}
	for dest, code := range want {
		a := attempts()[dest]
		if a.LastCode != code || a.LastAttemptAt == nil || a.Attempts < 1 {
			t.Errorf("%s: %+v, want code %s and a last attempt time", dest, a, code)
		}
	}

	fail.Store(false) // the EHR recovers; its code is cleared on success
	deadline := time.Now().Add(10 * time.Second)
	for attempts()["ehr"].LastCode != "" {
		if time.Now().After(deadline) {
			t.Fatalf("the code stayed after a success: %+v", attempts()["ehr"])
		}
		time.Sleep(50 * time.Millisecond)
	}
}
