package adapters

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net"
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
