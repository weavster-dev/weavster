package adapters

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReadFrame(t *testing.T) {
	for _, tt := range []struct {
		name, in, want string
		max            int
		err            error
	}{
		{"frame", "\x0bMSH|1\x1c\r", "MSH|1", 100, nil},
		{"leading bytes skipped", "\r\n\x0bMSH|1\x1c\r", "MSH|1", 100, nil},
		{"FS inside", "\x0ba\x1cb\x1c\r", "a\x1cb", 100, nil},
		{"FS FS CR", "\x0ba\x1c\x1c\r", "a\x1c", 100, nil},
		{"empty", "\x0b\x1c\r", "", 100, nil},
		{"too large", "\x0b" + strings.Repeat("x", 11) + "\x1c\r", strings.Repeat("x", 11), 10, ErrMLLPFrameTooLarge},
		{"too large keeps the first segment", "\x0bMSH|a\rPID|" + strings.Repeat("x", 20) + "\x1c\r", "MSH|a", 10, ErrMLLPFrameTooLarge},
		{"at the limit", "\x0b" + strings.Repeat("x", 10) + "\x1c\r", strings.Repeat("x", 10), 10, nil},
		{"cut off", "\x0bMSH", "", 100, io.EOF},
		{"cut off after FS", "\x0bMSH\x1c", "", 100, io.EOF},
		{"no start", "MSH", "", 100, io.EOF},
	} {
		got, err := readFrame(bufio.NewReaderSize(strings.NewReader(tt.in), 16), tt.max)
		if !errors.Is(err, tt.err) || string(got) != tt.want {
			t.Errorf("%s: %q, %v; want %q, %v", tt.name, got, err, tt.want, tt.err)
		}
	}
	// A large frame spans many buffer fills; the next frame still reads.
	big := strings.Repeat("y", 5000)
	r := bufio.NewReaderSize(strings.NewReader("\x0b"+big+"\x1c\r\x0bnext\x1c\r"), 16)
	if got, err := readFrame(r, 10000); err != nil || string(got) != big {
		t.Fatalf("big frame: %d bytes, %v", len(got), err)
	}
	if head, err := readFrame(bufio.NewReaderSize(strings.NewReader("\x0b"+big+"\x1c\r"), 16), 100); !errors.Is(err, ErrMLLPFrameTooLarge) || len(head) == 0 || len(head) > maxFrameHead || !strings.HasPrefix(big, string(head)) {
		t.Errorf("big frame over the limit: %d bytes kept, %v", len(head), err)
	}
	if got, err := readFrame(r, 10000); err != nil || string(got) != "next" {
		t.Errorf("frame after a big one: %q, %v", got, err)
	}
}

// readReply reads one framed reply from conn.
func readReply(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	got, err := readFrame(r, 1<<20)
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	return string(got)
}

// TestMLLPServer: frames on a connection are answered in order, several
// connections at once, an oversize frame gets a reply too, idle
// connections close, and Close finishes the frame being handled.
func TestMLLPServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	var mu sync.Mutex
	var seen []string
	srv := ServeMLLP(ln, func(frame []byte, err error) []byte {
		if errors.Is(err, ErrMLLPFrameTooLarge) {
			return []byte("too large")
		}
		if string(frame) == "slow" {
			<-release
		}
		mu.Lock()
		seen = append(seen, string(frame))
		mu.Unlock()
		return append([]byte("ack "), frame...)
	}, MLLPOptions{MaxFrame: 16, IdleTimeout: 300 * time.Millisecond, FrameTimeout: 5 * time.Second})

	dial := func() (net.Conn, *bufio.Reader) {
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		return conn, bufio.NewReader(conn)
	}
	a, ar := dial()
	b, br := dial()
	_, _ = a.Write(append(frameMLLP([]byte("one")), frameMLLP([]byte("two"))...))
	_, _ = b.Write(frameMLLP([]byte(strings.Repeat("z", 17))))
	if got := readReply(t, ar) + "|" + readReply(t, ar); got != "ack one|ack two" {
		t.Errorf("replies on a = %q", got)
	}
	if got := readReply(t, br); got != "too large" {
		t.Errorf("reply to an oversize frame = %q", got)
	}

	// Idle: the server closes b after the idle timeout.
	_ = b.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := br.ReadByte(); err == nil {
		t.Error("an idle connection stayed open")
	}

	// Close waits for the frame being handled and answers it (a new
	// connection: a idled out meanwhile).
	_ = a.Close()
	c, cr := dial()
	_, _ = c.Write(frameMLLP([]byte("slow")))
	time.Sleep(50 * time.Millisecond)
	closed := make(chan struct{})
	go func() { _ = srv.Close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("Close returned while a frame was being handled")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if got := readReply(t, cr); got != "ack slow" {
		t.Errorf("reply during Close = %q", got)
	}
	<-closed
	<-srv.Done()
	if _, err := net.DialTimeout("tcp", ln.Addr().String(), 200*time.Millisecond); err == nil {
		t.Error("the port still accepts after Close")
	}
	mu.Lock()
	defer mu.Unlock()
	if !bytes.Equal([]byte(strings.Join(seen, ",")), []byte("one,two,slow")) {
		t.Errorf("handled %v", seen)
	}
}

// TestMLLPServerSlowFrame: a frame that takes longer than the idle timeout
// to arrive is still answered, within the frame timeout.
func TestMLLPServerSlowFrame(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := ServeMLLP(ln, func(frame []byte, _ error) []byte { return frame }, MLLPOptions{MaxFrame: 100, IdleTimeout: 200 * time.Millisecond, FrameTimeout: 5 * time.Second})
	defer func() { _ = srv.Close() }()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	for _, part := range []string{"\x0bsl", "o", "w\x1c\r"} {
		_, _ = conn.Write([]byte(part))
		time.Sleep(150 * time.Millisecond) // 450 ms in all, over the idle timeout
	}
	if got := readReply(t, bufio.NewReader(conn)); got != "slow" {
		t.Errorf("reply = %q", got)
	}
}

// TestMLLPServerCloseCutsPartialFrame: Close does not wait for a frame that
// stopped arriving half-way (the frame timeout is long).
func TestMLLPServerCloseCutsPartialFrame(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := ServeMLLP(ln, func(frame []byte, _ error) []byte { return frame }, MLLPOptions{MaxFrame: 100, IdleTimeout: time.Minute, FrameTimeout: time.Hour})
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_, _ = conn.Write([]byte("\x0bhalf"))
	time.Sleep(100 * time.Millisecond)
	closed := make(chan struct{})
	go func() { _ = srv.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close waited for a partial frame")
	}
}

// TestMLLPServerTLS: on a tls listener, a client that completes the
// handshake is answered, and one that never starts it is closed after
// HandshakeTimeout rather than IdleTimeout.
func TestMLLPServerTLS(t *testing.T) {
	ts := httptest.NewUnstartedServer(nil) // for its 127.0.0.1 certificate
	ts.StartTLS()
	serverTLS, roots := ts.TLS.Clone(), ts.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	ts.Close()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", serverTLS)
	if err != nil {
		t.Fatal(err)
	}
	srv := ServeMLLP(ln, func(frame []byte, _ error) []byte { return frame },
		MLLPOptions{MaxFrame: 100, IdleTimeout: time.Minute, FrameTimeout: time.Minute, HandshakeTimeout: 200 * time.Millisecond})
	defer func() { _ = srv.Close() }()

	conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = conn.Write(frameMLLP([]byte("hello")))
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if reply, err := readFrame(bufio.NewReader(conn), 100); err != nil || string(reply) != "hello" {
		t.Errorf("reply over TLS = %q, %v", reply, err)
	}
	_ = conn.Close()

	stalled, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stalled.Close() }()
	start := time.Now()
	_ = stalled.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := stalled.Read(make([]byte, 1)); err == nil || time.Since(start) > 3*time.Second {
		t.Errorf("a client that never shakes hands: %v after %s", err, time.Since(start))
	}
}

// TestMLLPFraming: other start and end bytes delimit frames, a one-byte end
// ends the frame at once, and a lone first end byte inside the message is
// kept.
func TestMLLPFraming(t *testing.T) {
	for _, tt := range []struct {
		name, in string
		f        MLLPFraming
		want     []string
	}{
		{"default", "\x0bone\x1c\r\x0btwo\x1c\r", MLLPFraming{}, []string{"one", "two"}},
		{"one end byte", "\x02one\x03junk\x02two\x03", MLLPFraming{Start: 0x02, End: []byte{0x03}}, []string{"one", "two"}},
		{"two end bytes", "\x02a\x03b\x03\n", MLLPFraming{Start: 0x02, End: []byte{0x03, '\n'}}, []string{"a\x03b"}},
	} {
		r := bufio.NewReader(strings.NewReader(tt.in))
		var got []string
		for {
			frame, err := readFramed(r, 100, tt.f)
			if err != nil {
				break
			}
			got = append(got, string(frame))
		}
		if strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
		if w := tt.f.wrap([]byte("x")); string(w[1:2]) != "x" || (len(tt.f.End) > 0 && !bytes.HasSuffix(w, tt.f.End)) {
			t.Errorf("%s: wrap = %q", tt.name, w)
		}
	}
	if _, err := readFramed(bufio.NewReader(strings.NewReader("\x02"+strings.Repeat("x", 200)+"\x03")), 100, MLLPFraming{Start: 0x02, End: []byte{0x03}}); !errors.Is(err, ErrMLLPFrameTooLarge) {
		t.Errorf("an oversize frame with a one-byte end: %v", err)
	}
}

// TestMLLPServerNoReply: with NoReply the handler runs for every frame but
// nothing is written back.
func TestMLLPServerNoReply(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	handled := make(chan string, 2)
	f := MLLPFraming{Start: 0x02, End: []byte{0x03}}
	srv := ServeMLLP(ln, func(frame []byte, _ error) []byte { handled <- string(frame); return []byte("ack") },
		MLLPOptions{MaxFrame: 100, IdleTimeout: time.Minute, FrameTimeout: time.Minute, Framing: f, NoReply: true})
	defer func() { _ = srv.Close() }()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_, _ = conn.Write(append(f.wrap([]byte("one")), f.wrap([]byte("two"))...))
	for _, want := range []string{"one", "two"} {
		if got := <-handled; got != want {
			t.Errorf("handled %q, want %q", got, want)
		}
	}
	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if n, _ := conn.Read(make([]byte, 10)); n != 0 {
		t.Errorf("a reply was sent (%d bytes)", n)
	}
}
