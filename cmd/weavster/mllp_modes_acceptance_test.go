package main

import (
	"bufio"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestMLLPModes: mllp sources and destinations frame messages with the
// configured bytes, and with ackMode none a source replies nothing and a
// destination does not wait for an ACK.
func TestMLLPModes(t *testing.T) {
	addr, framed, silent := freeAddr(t), freeAddr(t), freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\n"+storeConfig(t))
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	msg := func(id string) string {
		return "MSH|^~\\&|LAB|HOSP|WEAVSTER|HOSP|20260927120000||ADT^A01|" + id + "|P|2.5\rPID|1||12345||DOE^JOHN\r"
	}

	// A source framing messages STX … ETX answers in the same framing.
	createFlow(t, c, `{"id":"framed","source":{"type":"mllp","address":"`+framed+`","frameStart":"02","frameEnd":"03"}}`)
	m := dialMLLP(t, framed)
	defer func() { _ = m.conn.Close() }()
	_, _ = m.conn.Write([]byte("\x02" + msg("F1") + "\x03"))
	_ = m.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if b, err := m.r.ReadByte(); err != nil || b != 0x02 {
		t.Fatalf("ACK start: %x %v", b, err)
	}
	ack, err := m.r.ReadString(0x03)
	if err != nil || !strings.Contains(ack, "MSA|AA|F1") {
		t.Errorf("framed ACK = %q, %v", ack, err)
	}

	// A source with ackMode none stores the message and replies nothing.
	createFlow(t, c, `{"id":"silent","source":{"type":"mllp","address":"`+silent+`","ackMode":"none"}}`)
	s := dialMLLP(t, silent)
	defer func() { _ = s.conn.Close() }()
	_, _ = s.conn.Write([]byte("\x0b" + msg("S1") + "\x1c\r"))
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, body, _ := c.do(http.MethodGet, "/api/v1/messages?flowId=silent", "", admin); strings.Contains(body, `"source.mllp.controlId":"S1"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the message sent to the silent source was not stored")
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = s.conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if n, _ := s.conn.Read(make([]byte, 64)); n != 0 {
		t.Errorf("a source with ackMode none replied %d bytes", n)
	}

	// Destinations: one framing STX … ETX and waiting for its ACK, one
	// sending to a receiver that never answers.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	got := make(chan string, 2)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			br := bufio.NewReader(conn)
			start, _ := br.ReadByte()
			body, _ := br.ReadString(0x03)
			got <- string(start) + body
			if strings.Contains(body, "|D1|") {
				_, _ = conn.Write([]byte("\x02MSH|^~\\&|LAB|H|W|H|20260927120000||ACK^A01|A1|P|2.5\rMSA|AA|D1\r\x03"))
			}
			_ = conn.Close()
		}
	}()
	createFlow(t, c, `{"id":"out","inputFormat":"hl7v2","destinations":[{"name":"lab","type":"mllp","address":"`+ln.Addr().String()+`","frameStart":"02","frameEnd":"03","timeoutMs":5000}]}`)
	if _, status := sendMessage(t, c, "out", msg("D1")); status != "sent" {
		t.Errorf("status with framing = %s, want sent", status)
	}
	if r := <-got; r != "\x02"+msg("D1")+"\x03" {
		t.Errorf("the receiver got %q", r)
	}
	createFlow(t, c, `{"id":"fire","inputFormat":"hl7v2","destinations":[{"name":"lab","type":"mllp","address":"`+ln.Addr().String()+`","frameStart":"02","frameEnd":"03","ackMode":"none","timeoutMs":5000}]}`)
	if _, status := sendMessage(t, c, "fire", msg("N1")); status != "sent" {
		t.Errorf("status without an ACK = %s, want sent", status)
	}
	if r := <-got; !strings.Contains(r, "|N1|") {
		t.Errorf("the receiver got %q", r)
	}

	for body, want := range map[string]string{
		`{"id":"x","source":{"type":"mllp","address":"` + freeAddr(t) + `","frameEnd":"0D"}}`:                                   "frameEnd starts with 0D",
		`{"id":"x","source":{"type":"mllp","address":"` + freeAddr(t) + `","frameStart":"1C"}}`:                                 "must differ",
		`{"id":"x","source":{"type":"mllp","address":"` + freeAddr(t) + `","ackMode":"commit"}}`:                                "flow.schema.json",
		`{"id":"x","inputFormat":"hl7v2","destinations":[{"name":"lab","type":"mllp","address":"lab:2575","frameStart":"41"}]}`: "frameStart 41 can occur in a message",
		`{"id":"x","destinations":[{"name":"out","type":"file","dir":"/tmp/x","ackMode":"none"}]}`:                              "flow.schema.json",
	} {
		if code, resp, _ := c.do(http.MethodPost, "/api/v1/flows", body, admin); code != http.StatusBadRequest || !strings.Contains(resp, want) {
			t.Errorf("%s: %d %s", body, code, resp)
		}
	}
}
