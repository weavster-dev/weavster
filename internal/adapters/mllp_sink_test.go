package adapters

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// mllpPeer accepts one connection per reply and answers the frame it reads
// with reply (nothing when reply is "").
func mllpPeer(t *testing.T, replies ...string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for _, reply := range replies {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			if _, err := readFrame(bufio.NewReader(conn), 1<<20); err == nil && reply != "" {
				_, _ = conn.Write(frameMLLP([]byte(reply)))
			}
			if reply == "" {
				time.Sleep(300 * time.Millisecond)
			}
			_ = conn.Close()
		}
	}()
	return ln.Addr().String()
}

// TestMLLPSinkACK: the ACK decides the delivery.
func TestMLLPSinkACK(t *testing.T) {
	msg := "MSH|^~\\&|A|B|C|D|1||ADT^A01|C1|P|2.5\rPID|1\r"
	ack := func(code, id string) string {
		return "MSH|^~\\&|C|D|A|B|2||ACK^A01|X|P|2.5\rMSA|" + code + "|" + id + "\r"
	}
	for _, tt := range []struct {
		name, reply, want string
	}{
		{"AA", ack("AA", "C1"), ""},
		{"CA", ack("CA", "C1"), ""},
		{"AE", ack("AE", "C1"), "mllp: ACK AE (application error)"},
		{"CR", ack("CR", "C1"), "mllp: ACK CR (application reject)"},
		{"unknown code", ack("ZZ", "C1"), "mllp: ACK with an unknown code"},
		{"other message", ack("AA", "C2"), "mllp: the ACK is for another message"},
		{"not an ACK", "MSH|^~\\&|C\rPID|1\r", "mllp: the reply is not an HL7 ACK"},
		{"no reply", "", "mllp: no ACK"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := NewMLLPSinkWith(mllpPeer(t, tt.reply), 200*time.Millisecond).Write(context.Background(), Message{Body: []byte(msg)})
			if (tt.want == "") != (err == nil) || (err != nil && !strings.HasPrefix(err.Error(), tt.want)) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
	if err := NewMLLPSink("127.0.0.1:1").Write(context.Background(), Message{Body: []byte(msg)}); err == nil {
		t.Error("delivery to a closed port succeeded")
	}
	if s := NewMLLPSinkWith("x:1", -time.Second); s.timeout != MLLPSinkTimeout {
		t.Errorf("negative timeout gave %v", s.timeout)
	}
}
