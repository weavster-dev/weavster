package main

import (
	"bufio"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// mllpClient sends MLLP frames on one connection and reads the ACKs.
type mllpClient struct {
	t    *testing.T
	conn net.Conn
	r    *bufio.Reader
}

func dialMLLP(t *testing.T, addr string) *mllpClient {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			return &mllpClient{t: t, conn: conn, r: bufio.NewReader(conn)}
		}
		if time.Now().After(deadline) {
			t.Fatalf("mllp source %s never listened", addr)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// send writes one frame and returns the ACK's segments.
func (c *mllpClient) send(msg string) []string {
	c.t.Helper()
	if _, err := c.conn.Write([]byte("\x0b" + msg + "\x1c\r")); err != nil {
		c.t.Fatal(err)
	}
	_ = c.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if b, err := c.r.ReadByte(); err != nil || b != 0x0b {
		c.t.Fatalf("ACK start: %v %v", b, err)
	}
	ack, err := c.r.ReadString(0x1c)
	if err != nil {
		c.t.Fatal(err)
	}
	if b, _ := c.r.ReadByte(); b != '\r' {
		c.t.Fatalf("ACK end: %v", b)
	}
	return strings.Split(strings.TrimRight(strings.TrimSuffix(ack, "\x1c"), "\r"), "\r")
}

// TestMLLPSource: a started flow with an mllp source accepts HL7 v2 over
// TCP, stores and delivers each message, and answers each with an ACK (AA
// stored, AR refused); the port follows the flow's lifecycle.
func TestMLLPSource(t *testing.T) {
	addr, src, strict := freeAddr(t), freeAddr(t), freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	out := t.TempDir()
	createFlow(t, c, `{"id":"adt","source":{"type":"mllp","address":"`+src+`"},"destinations":[{"name":"out","type":"file","dir":"`+out+`"}]}`)

	adt := func(id string) string {
		return "MSH|^~\\&|LAB|HOSP|WEAVSTER|HOSP|20260927120000||ADT^A01|" + id + "|P|2.5\rPID|1||12345||DOE^JOHN\r"
	}
	m := dialMLLP(t, src)
	defer func() { _ = m.conn.Close() }()
	for _, id := range []string{"MSG1", "MSG2"} {
		ack := m.send(adt(id))
		if len(ack) != 2 || !strings.HasPrefix(ack[0], "MSH|^~\\&|WEAVSTER|HOSP|LAB|HOSP|") || !strings.Contains(ack[0], "|ACK^A01|") || ack[1] != "MSA|AA|"+id {
			t.Errorf("ACK for %s = %q", id, ack)
		}
		if ts := strings.Split(ack[0], "|")[6]; !strings.HasPrefix(ts, time.Now().Format("2006")) || ts == "20260927120000" {
			t.Errorf("ACK MSH-7 = %q, want the current time", ts)
		}
	}
	if entries, _ := os.ReadDir(out); len(entries) != 2 {
		t.Errorf("delivered %d files, want 2", len(entries))
	}
	if _, body, _ := c.do(http.MethodGet, "/api/v1/messages?flowId=adt", "", admin); !strings.Contains(body, `"source.mllp.controlId":"MSG1"`) {
		t.Errorf("messages = %s", body)
	}
	if ack := m.send("hello"); len(ack) != 2 || ack[1] != "MSA|AR||not an HL7 v2 message (no MSH segment)" {
		t.Errorf("ACK for non-HL7 = %q", ack)
	}

	// A flow that refuses the message (its transform needs JSON) answers AR.
	createFlow(t, c, `{"id":"strict","source":{"type":"mllp","address":"`+strict+`"},"transform":{"name":"t","steps":[{"set":{"field":"x","expr":"1"}}]}}`)
	s := dialMLLP(t, strict)
	defer func() { _ = s.conn.Close() }()
	if ack := s.send(adt("MSG3")); len(ack) != 2 || ack[1] != "MSA|AR|MSG3|message refused by the flow" {
		t.Errorf("ACK for a refused message = %q", ack)
	}

	if _, body, _ := c.do(http.MethodGet, "/api/v1/flows/ports-in-use", "", admin); !strings.Contains(body, `"usedBy":"flow:adt"`) {
		t.Errorf("ports-in-use = %s", body)
	}
	if _, body, _ := c.do(http.MethodGet, "/api/v1/flows/connector-names", "", admin); !strings.Contains(body, `"id":"adt","name":"","sourceType":"mllp"`) {
		t.Errorf("connector-names = %s", body)
	}

	// Stopping the flow closes the port (the open connection too).
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows/adt/stop", "", admin); code != http.StatusOK {
		t.Fatalf("stop: %d %s", code, body)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", src, 200*time.Millisecond)
		if err != nil {
			break
		}
		_ = conn.Close()
		if time.Now().After(deadline) {
			t.Fatal("the port stayed open after stop")
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = m.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := m.r.ReadByte(); err == nil {
		t.Error("an open connection survived the stop")
	}
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"twin","source":{"type":"mllp","address":"`+strict+`"}}`, admin); code != http.StatusBadRequest || !strings.Contains(body, "a port can have one flow source") {
		t.Errorf("second flow on the same port: %d %s", code, body)
	}
}
