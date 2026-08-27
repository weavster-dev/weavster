package adapters

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"strings"
	"testing"
)

// TestSMTPSinkWriteError covers the error branch of SMTPSink.Write, which was
// only exercised on the happy path. A failing send function must surface its
// error to the caller rather than being swallowed.
func TestSMTPSinkWriteError(t *testing.T) {
	wantErr := errors.New("smtp: dial failure")
	sink := NewSMTPSink("localhost:25", "from@example.com", []string{"to@example.com"})
	sink.send = func(_ string, _ smtp.Auth, _ string, _ []string, _ []byte) error {
		return wantErr
	}
	if err := sink.Write(context.Background(), Message{Body: []byte("body")}); err != wantErr {
		t.Errorf("Write() error = %v, want %v", err, wantErr)
	}
}

// TestWebServiceSinkNewRequestError covers the error branch of
// WebServiceSink.Write where http.NewRequestWithContext fails (invalid URL
// control character), which the happy-path SOAPAction test never exercises.
func TestWebServiceSinkNewRequestError(t *testing.T) {
	sink := NewWebServiceSink("http://example.com/\x00", "urn:test")
	if err := sink.Write(context.Background(), Message{Body: []byte("<soap/>")}); err == nil {
		t.Error("Write() expected error for invalid URL, got nil")
	}
}

// TestWebServiceSinkDoError covers the error branch of WebServiceSink.Write
// where the HTTP client fails to perform the request.
func TestWebServiceSinkDoError(t *testing.T) {
	sink := NewWebServiceSink("http://example.invalid", "urn:test")
	sink.client = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("connection refused")
		}),
	}
	if err := sink.Write(context.Background(), Message{Body: []byte("<soap/>")}); err == nil {
		t.Error("Write() expected transport error, got nil")
	}
}

// TestWebServiceSinkNon2xx covers the non-2xx status branch of
// WebServiceSink.Write, which returns an httpStatusError.
func TestWebServiceSinkNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	sink := NewWebServiceSink(srv.URL, "urn:test")
	err := sink.Write(context.Background(), Message{Body: []byte("<soap/>")})
	if err == nil {
		t.Fatal("Write() expected non-2xx error, got nil")
	}
	var statusErr *httpStatusError
	if !errors.As(err, &statusErr) {
		t.Errorf("Write() error type = %T, want *httpStatusError", err)
	}
	if statusErr.code != http.StatusInternalServerError {
		t.Errorf("statusErr.code = %d, want %d", statusErr.code, http.StatusInternalServerError)
	}
}

// TestWebServiceSinkEmptySOAPAction covers the branch where the SOAPAction
// header is not set because the configured action is empty.
func TestWebServiceSinkEmptySOAPAction(t *testing.T) {
	var got []string
	var present bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, present = r.Header[http.CanonicalHeaderKey("SOAPAction")]
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sink := NewWebServiceSink(srv.URL, "")
	if err := sink.Write(context.Background(), Message{Body: []byte("<soap/>")}); err != nil {
		t.Fatal(err)
	}
	if present {
		t.Errorf("SOAPAction header set to %v, want absent", got)
	}
}

// roundTripFunc adapts a plain function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestInterflowSourceReadEOF covers the closed-channel (io.EOF) branch of
// interflowSource.Read, which the happy-path and cancelled-context tests
// never exercise.
func TestInterflowSourceReadEOF(t *testing.T) {
	f := NewInterflow(1)
	sink := f.Sink()
	src := f.Source()
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Read(context.Background()); err != io.EOF {
		t.Errorf("Read() after close = %v, want io.EOF", err)
	}
}

// TestInterflowSinkWriteContextCancelled covers the cancelled-context branch
// of interflowSink.Write on an unbuffered pipe.
func TestInterflowSinkWriteContextCancelled(t *testing.T) {
	f := NewInterflow(0) // unbuffered so write blocks
	sink := f.Sink()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := sink.Write(ctx, Message{Body: []byte("x")}); err == nil {
		t.Error("Write() expected context error, got nil")
	}
}

// TestReadMLLPFrameErrorPaths covers the io.ReadFull error branches of
// readMLLPFrame, which the happy-path MLLP framing test never exercises.
func TestReadMLLPFrameErrorPaths(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"empty reader", []byte{}},
		{"missing start byte", []byte("X")},
		{"truncated body", []byte{0x0B, 'A', 'B'}},
		{"trailing FS without CR", []byte{0x0B, 'A', 0x1C}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := readMLLPFrame(strings.NewReader(string(c.data))); err == nil {
				t.Errorf("readMLLPFrame(%v) expected error, got nil", c.data)
			}
		})
	}
}

// TestReadMLLPFrameWrongStartByte covers the unexpected start-byte branch,
// which returns io.ErrUnexpectedEOF rather than io.EOF.
func TestReadMLLPFrameWrongStartByte(t *testing.T) {
	if _, err := readMLLPFrame(strings.NewReader("X")); err != io.ErrUnexpectedEOF {
		t.Errorf("readMLLPFrame(X) error = %v, want io.ErrUnexpectedEOF", err)
	}
}

// TestReadMLLPFrameFSNotTerminator covers the branch where a frame-start
// (FS, 0x1C) byte is followed by a non-CR byte, so it is treated as body
// content rather than the frame terminator.
func TestReadMLLPFrameFSNotTerminator(t *testing.T) {
	data := []byte{0x0B, 0x1C, 0x20, 0x1C, 0x0D}
	got, err := readMLLPFrame(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string([]byte{0x1C, 0x20}) {
		t.Errorf("readMLLPFrame body = %v, want %v", got, []byte{0x1C, 0x20})
	}
}

// TestMLLPSinkDialError covers the dialer-error branch of MLLPSink.Write
// where the sink has not yet established a connection.
func TestMLLPSinkDialError(t *testing.T) {
	wantErr := errors.New("dial failure")
	sink := NewMLLPSink("127.0.0.1:0")
	sink.dialer = func(_ context.Context, _ string) (net.Conn, error) {
		return nil, wantErr
	}
	if err := sink.Write(context.Background(), Message{Body: []byte("body")}); err != wantErr {
		t.Errorf("Write() error = %v, want %v", err, wantErr)
	}
}

// TestMLLPSinkWriteError covers the connection-write-error branch of
// MLLPSink.Write where an established connection fails on write.
func TestMLLPSinkWriteError(t *testing.T) {
	sink := NewMLLPSink("127.0.0.1:0")
	sink.conn = &failingConn{err: errors.New("write failure")}
	if err := sink.Write(context.Background(), Message{Body: []byte("body")}); err == nil {
		t.Error("Write() expected conn write error, got nil")
	}
}

// TestMLLPSourceReadFrameError covers the MLLPSource.Read branch where
// readMLLPFrame fails on an accepted connection, surfacing the error.
func TestMLLPSourceReadFrameError(t *testing.T) {
	src, err := ListenMLLP("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = src.Close() }()

	wantErr := errors.New("read failure")
	src.conn = &failingConn{err: wantErr}
	if _, err := src.Read(context.Background()); err != wantErr {
		t.Errorf("Read() error = %v, want %v", err, wantErr)
	}
}

// TestMLLPSourceReadAcceptError covers the MLLPSource.Read branch where the
// listener is closed and Accept fails, surfacing the error to the caller.
func TestMLLPSourceReadAcceptError(t *testing.T) {
	src, err := ListenMLLP("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := src.listener.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Read(context.Background()); err == nil {
		t.Error("Read() expected accept error, got nil")
	}
}

// failingConn is a net.Conn whose Read and Write always fail with err.
type failingConn struct {
	net.Conn
	err error
}

func (c *failingConn) Write([]byte) (int, error) { return 0, c.err }
func (c *failingConn) Read([]byte) (int, error)  { return 0, c.err }
func (c *failingConn) Close() error              { return nil }
