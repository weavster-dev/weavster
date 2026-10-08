package adapters

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
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
			if _, err := readFramed(bufio.NewReader(conn), 1<<20, MLLPFraming{}); err == nil && reply != "" {
				_, _ = conn.Write(MLLPFraming{}.wrap([]byte(reply)))
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
		{"AR without an id", ack("AR", ""), "mllp: ACK AR (application reject)"},
		{"unknown code", ack("ZZ", "C1"), `mllp: ACK with an unknown code "ZZ"`},
		{"long unknown code", ack("ABCDEFGHIJKL", "C1"), `mllp: ACK with an unknown code "ABCDEFGH"`},
		{"MSA without MSH", "MSA|AA|C1\r", "mllp: the reply is not an HL7 ACK"},
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
	if err := NewMLLPSink("127.0.0.1:1").Write(context.Background(), Message{Body: []byte("MSH|^~\\&|A\x1c\rPID|1")}); err == nil || !strings.Contains(err.Error(), "end bytes (1C0D)") {
		t.Errorf("a body with FS CR: %v", err)
	}
	// Cancelling the caller stops the wait for the ACK at once.
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	if err := NewMLLPSinkWith(mllpPeer(t, ""), 10*time.Second).Write(ctx, Message{Body: []byte(msg)}); err == nil || time.Since(start) > 2*time.Second {
		t.Errorf("cancelled delivery: %v after %v", err, time.Since(start))
	}
	if s := NewMLLPSinkWith("x:1", -time.Second); s.timeout != MLLPSinkTimeout {
		t.Errorf("negative timeout gave %v", s.timeout)
	}
}

// TestMLLPSinkTLS: a TLS sink delivers to a receiver it trusts and refuses
// one whose certificate it cannot verify, before sending anything.
func TestMLLPSinkTLS(t *testing.T) {
	ts := httptest.NewUnstartedServer(nil) // for its 127.0.0.1 certificate
	ts.StartTLS()
	serverTLS, roots := ts.TLS.Clone(), ts.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	ts.Close()
	msg := "MSH|^~\\&|A|B|C|D|1||ADT^A01|C1|P|2.5\rPID|1\r"
	for _, tt := range []struct {
		name  string
		roots *x509.CertPool
		want  string
	}{
		{"trusted", roots, ""},
		{"untrusted", x509.NewCertPool(), "certificate"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ln, err := tls.Listen("tcp", "127.0.0.1:0", serverTLS)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = ln.Close() })
			got := make(chan bool, 1) // whether the receiver read a frame
			go func() {
				conn, err := ln.Accept()
				if err != nil {
					got <- false
					return
				}
				defer func() { _ = conn.Close() }()
				_, err = readFramed(bufio.NewReader(conn), 1<<20, MLLPFraming{})
				got <- err == nil
				if err == nil {
					_, _ = conn.Write(MLLPFraming{}.wrap([]byte("MSH|^~\\&|C|D|A|B|2||ACK^A01|X|P|2.5\rMSA|AA|C1\r")))
				}
			}()
			cfg := &tls.Config{RootCAs: tt.roots, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12}
			err = NewMLLPSinkTLS(ln.Addr().String(), 5*time.Second, cfg).Write(context.Background(), Message{Body: []byte(msg)})
			if (tt.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tt.want)) {
				t.Errorf("Write = %v, want %q", err, tt.want)
			}
			if read := <-got; read != (tt.want == "") {
				t.Errorf("the receiver read a frame: %v", read)
			}
		})
	}
}

// TestMLLPSinkMode: a sink with other framing sends and reads the ACK in
// it; without ACKs a written message is delivered.
func TestMLLPSinkMode(t *testing.T) {
	msg := "MSH|^~\\&|A|B|C|D|1||ADT^A01|C1|P|2.5\rPID|1\r"
	f := MLLPFraming{Start: 0x02, End: []byte{0x03}}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	got := make(chan string, 2)
	go func() {
		for i := 0; i < 2; i++ {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			frame, err := readFramed(bufio.NewReader(conn), 1<<20, f)
			got <- string(frame)
			if err == nil && i == 0 {
				_, _ = conn.Write(f.wrap([]byte("MSH|^~\\&|C|D|A|B|2||ACK^A01|X|P|2.5\rMSA|AA|C1\r")))
			}
			_ = conn.Close()
		}
	}()
	if err := NewMLLPSinkWith(ln.Addr().String(), 5*time.Second).WithMode(f, false).Write(context.Background(), Message{Body: []byte(msg)}); err != nil {
		t.Errorf("framed delivery: %v", err)
	}
	if err := NewMLLPSinkWith(ln.Addr().String(), 5*time.Second).WithMode(f, true).Write(context.Background(), Message{Body: []byte(msg)}); err != nil {
		t.Errorf("delivery without an ACK: %v", err)
	}
	for i := 0; i < 2; i++ {
		if m := <-got; m != msg {
			t.Errorf("received %q", m)
		}
	}
	if err := NewMLLPSink("127.0.0.1:1").WithMode(f, true).Write(context.Background(), Message{Body: []byte("a\x03b")}); err == nil || !strings.Contains(err.Error(), "end bytes (03)") {
		t.Errorf("a body with the end byte: %v", err)
	}
	if err := NewMLLPSink("127.0.0.1:1").Write(context.Background(), Message{Body: []byte("MSH|^~\\&|A\x0bB")}); err == nil || !strings.Contains(err.Error(), "start byte (0B)") {
		t.Errorf("a body with the start byte: %v", err)
	}
}

// TestMLLPSinkDrain: without ACKs, waiting for the receiver to close ends
// at the delivery's deadline, or at once when the caller is gone.
func TestMLLPSinkDrain(t *testing.T) {
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
			_, _ = conn.Write([]byte("hello")) // then never closes
			t.Cleanup(func() { _ = conn.Close() })
		}
	}()
	for _, tt := range []struct {
		name    string
		timeout time.Duration
		cancel  bool
	}{
		{"deadline", 200 * time.Millisecond, false},
		{"cancelled", 5 * time.Second, true},
	} {
		c, _ := net.Dial("tcp", ln.Addr().String())
		ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
		if tt.cancel {
			cancel()
		}
		start := time.Now()
		drain(ctx, c)
		cancel()
		_ = c.Close()
		if time.Since(start) > 600*time.Millisecond {
			t.Errorf("%s: drained for %s", tt.name, time.Since(start))
		}
	}
}

// TestMLLPSinkACKDecoded: control ids are compared decoded, so an ACK in
// the standard delimiters matches a message with its own.
func TestMLLPSinkACKDecoded(t *testing.T) {
	msg := "MSH#$%!@#A#B#C#D#1##ADT$A01#M!F!2#P#2.4\rPID#1\r"
	peer := mllpPeer(t, "MSH|^~\\&|C|D|A|B|2||ACK^A01|X|P|2.4\rMSA|AA|M#2\r")
	if err := NewMLLPSinkWith(peer, 5*time.Second).Write(context.Background(), Message{Body: []byte(msg)}); err != nil {
		t.Errorf("Write = %v", err)
	}
}
