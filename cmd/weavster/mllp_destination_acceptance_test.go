package main

import (
	"bufio"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// mllpReceiver is a test HL7 system: it answers each MLLP frame with the
// next ACK code from codes (the last one repeats), echoing MSH-10 in MSA-2
// unless wrongID is set, and records what it received.
type mllpReceiver struct {
	mu       sync.Mutex
	codes    []string
	wrongID  bool
	received []string
}

func (r *mllpReceiver) serve(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				br := bufio.NewReader(conn)
				if b, err := br.ReadByte(); err != nil || b != 0x0b {
					return
				}
				msg, err := br.ReadString(0x1c)
				if err != nil {
					return
				}
				_, _ = br.ReadByte()
				msg = strings.TrimSuffix(msg, "\x1c")
				r.mu.Lock()
				r.received = append(r.received, msg)
				code := r.codes[min(len(r.received), len(r.codes))-1]
				r.mu.Unlock()
				id := strings.Split(strings.Split(msg, "\r")[0], "|")[9]
				if r.wrongID {
					id = "OTHER"
				}
				_, _ = conn.Write([]byte("\x0bMSH|^~\\&|LAB|H|W|H|20260927120000||ACK^A01|A1|P|2.5\rMSA|" + code + "|" + id + "\r\x1c\r"))
			}()
		}
	}()
	return ln.Addr().String()
}

func (r *mllpReceiver) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.received)
}

// TestMLLPDestination: an mllp destination delivers HL7 v2 over TCP and the
// receiver's ACK decides the result: AA is sent, AE is retried until AA,
// and an ACK for another message never counts.
func TestMLLPDestination(t *testing.T) {
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n"+
		"delivery: {maxAttempts: 50, backoffBaseMs: 10, retryIntervalMs: 20}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	msg := "MSH|^~\\&|W|H|LAB|H|20260927120000||ADT^A01|MSG1|P|2.5\rPID|1||123||DOE^JOHN\r"

	ok := &mllpReceiver{codes: []string{"AA"}}
	createFlow(t, c, `{"id":"ok","inputFormat":"hl7v2","destinations":[{"name":"lab","type":"mllp","address":"`+ok.serve(t)+`","timeoutMs":5000}]}`)
	if _, status := sendMessage(t, c, "ok", msg); status != "sent" {
		t.Errorf("status = %s, want sent", status)
	}
	if ok.count() != 1 || ok.received[0] != msg {
		t.Errorf("received %q", ok.received)
	}

	busy := &mllpReceiver{codes: []string{"AE", "AE", "AA"}}
	createFlow(t, c, `{"id":"busy","inputFormat":"hl7v2","destinations":[{"name":"lab","type":"mllp","address":"`+busy.serve(t)+`"}]}`)
	id, status := sendMessage(t, c, "busy", msg)
	if status != "queued" {
		t.Errorf("status after AE = %s, want queued", status)
	}
	waitStatus(t, c, id, "sent")
	if busy.count() != 3 {
		t.Errorf("deliveries = %d, want 3 (AE, AE, AA)", busy.count())
	}

	wrong := &mllpReceiver{codes: []string{"AA"}, wrongID: true}
	createFlow(t, c, `{"id":"wrong","inputFormat":"hl7v2","destinations":[{"name":"lab","type":"mllp","address":"`+wrong.serve(t)+`"}]}`)
	if _, status := sendMessage(t, c, "wrong", msg); status != "queued" {
		t.Errorf("status with an ACK for another message = %s, want queued", status)
	}

	for body, want := range map[string]string{
		`{"id":"x","inputFormat":"hl7v2","destinations":[{"name":"lab","type":"mllp","address":"lab.example"}]}`: "address must be host:port",
		`{"id":"x","destinations":[{"name":"lab","type":"mllp","address":"lab.example:2575"}]}`:                  "needs inputFormat hl7v2",
		`{"id":"x","destinations":[{"name":"out","type":"http","url":"https://x","address":"lab:2575"}]}`:        "flow.schema.json",
		`{"id":"x","destinations":[{"name":"lab","type":"mllp","address":"lab.example:2575","method":"PUT"}]}`:   "flow.schema.json",
	} {
		if code, resp, _ := c.do(http.MethodPost, "/api/v1/flows", body, admin); code != http.StatusBadRequest || !strings.Contains(resp, want) {
			t.Errorf("%s: %d %s", body, code, resp)
		}
	}
}
