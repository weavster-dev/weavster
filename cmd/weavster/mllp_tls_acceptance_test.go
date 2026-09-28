package main

import (
	"bufio"
	"crypto/tls"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestMLLPTLS: an mllp source with a certificate accepts MLLP over TLS only,
// and an mllp destination with tls delivers over TLS, trusting caFile, and
// never to a receiver whose certificate it cannot verify.
func TestMLLPTLS(t *testing.T) {
	addr, src := freeAddr(t), freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n"+
		"delivery: {maxAttempts: 50, backoffBaseMs: 10, retryIntervalMs: 20}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	certFile, keyFile, pool := selfSignedCert(t, t.TempDir())
	msg := "MSH|^~\\&|LAB|HOSP|WEAVSTER|HOSP|20260927120000||ADT^A01|MSG1|P|2.5\rPID|1||12345||DOE^JOHN\r"

	// The source: TLS clients get their ACK; a plain TCP client gets none.
	createFlow(t, c, `{"id":"secure-in","source":{"type":"mllp","address":"`+src+`","certFile":"`+certFile+`","keyFile":"`+keyFile+`"}}`)
	m := dialMLLP(t, src) // waits for the port
	_ = m.conn.Close()
	conn, err := tls.Dial("tcp", src, &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatalf("TLS handshake with the mllp source: %v", err)
	}
	secure := &mllpClient{t: t, conn: conn, r: bufio.NewReader(conn)}
	if ack := secure.send(msg); len(ack) != 2 || ack[1] != "MSA|AA|MSG1" {
		t.Errorf("ACK over TLS = %q", ack)
	}
	_ = conn.Close()
	plain, err := net.Dial("tcp", src)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = plain.Write([]byte("\x0b" + msg + "\x1c\r"))
	_ = plain.SetReadDeadline(time.Now().Add(5 * time.Second))
	reply := make([]byte, 4096)
	n, _ := plain.Read(reply)
	_ = plain.Close()
	if strings.Contains(string(reply[:n]), "MSA|") {
		t.Errorf("a plain TCP client got an ACK: %q", reply[:n])
	}
	if _, body, _ := c.do(http.MethodGet, "/api/v1/messages?flowId=secure-in", "", admin); strings.Count(body, `"source.mllp.controlId"`) != 1 {
		t.Errorf("stored messages: %s", body)
	}

	// The destination: caFile trusts the receiver's certificate.
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	receiverTLS := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	lab := &mllpReceiver{codes: []string{"AA"}, tls: receiverTLS}
	createFlow(t, c, `{"id":"secure-out","inputFormat":"hl7v2","destinations":[{"name":"lab","type":"mllp","address":"`+lab.serve(t)+`","tls":true,"caFile":"`+certFile+`"}]}`)
	if _, status := sendMessage(t, c, "secure-out", msg); status != "sent" {
		t.Errorf("status over TLS = %s, want sent", status)
	}
	if lab.count() != 1 {
		t.Errorf("the TLS receiver got %d messages, want 1", lab.count())
	}

	// Without caFile the system's roots do not trust the self-signed
	// receiver, and a plain destination cannot talk to a TLS receiver.
	untrusted := &mllpReceiver{codes: []string{"AA"}, tls: receiverTLS}
	createFlow(t, c, `{"id":"untrusted","inputFormat":"hl7v2","destinations":[{"name":"lab","type":"mllp","address":"`+untrusted.serve(t)+`","tls":true,"timeoutMs":2000}]}`)
	if _, status := sendMessage(t, c, "untrusted", msg); status != "queued" {
		t.Errorf("status with an untrusted receiver = %s, want queued", status)
	}
	if untrusted.count() != 0 {
		t.Errorf("an unverified receiver got %d messages", untrusted.count())
	}

	for body, want := range map[string]string{
		`{"id":"x","source":{"type":"mllp","address":"` + freeAddr(t) + `","certFile":"` + certFile + `"}}`:                                "flow.schema.json",
		`{"id":"x","source":{"type":"mllp","address":"` + freeAddr(t) + `","certFile":"cert.pem","keyFile":"key.pem"}}`:                    "must be absolute paths",
		`{"id":"x","inputFormat":"hl7v2","destinations":[{"name":"lab","type":"mllp","address":"lab:2575","caFile":"` + certFile + `"}]}`:  "caFile needs tls: true",
		`{"id":"x","inputFormat":"hl7v2","destinations":[{"name":"lab","type":"mllp","address":"lab:2575","tls":true,"caFile":"ca.pem"}]}`: "caFile must be an absolute path",
		`{"id":"x","destinations":[{"name":"out","type":"http","url":"https://x","tls":true}]}`:                                            "flow.schema.json",
	} {
		if code, resp, _ := c.do(http.MethodPost, "/api/v1/flows", body, admin); code != http.StatusBadRequest || !strings.Contains(resp, want) {
			t.Errorf("%s: %d %s", body, code, resp)
		}
	}
}
