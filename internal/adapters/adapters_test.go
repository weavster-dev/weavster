package adapters

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestFileSourceAndSink(t *testing.T) {
	dir := t.TempDir()
	sink := NewFileSink(dir)
	if err := sink.Write(context.Background(), Message{ID: "a.txt", Body: []byte("hello")}); err != nil {
		t.Fatal(err)
	}

	src, err := NewFileSource(dir, "*.txt")
	if err != nil {
		t.Fatal(err)
	}
	m, err := src.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "a.txt" || string(m.Body) != "hello" {
		t.Errorf("read = %+v", m)
	}
	if _, err := src.Read(context.Background()); err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestMLLPFrame(t *testing.T) {
	frame := frameMLLP([]byte("MSH|..."))
	if frame[0] != mllpStart || frame[len(frame)-1] != mllpEnd[1] || frame[len(frame)-2] != mllpEnd[0] {
		t.Fatalf("bad frame: %v", frame)
	}
	body, err := readMLLPFrame(bytes.NewReader(frame))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "MSH|..." {
		t.Errorf("body = %q", body)
	}
}

func TestMLLPSinkWritesFramed(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()

	got := make(chan []byte, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		body, err := readMLLPFrame(conn)
		if err != nil {
			return
		}
		got <- body
	}()

	sink := NewMLLPSink(l.Addr().String())
	if err := sink.Write(context.Background(), Message{Body: []byte("ADT^A01")}); err != nil {
		t.Fatal(err)
	}
	if body := <-got; string(body) != "ADT^A01" {
		t.Errorf("received = %q", body)
	}
	_ = sink.Close()
}

func TestHTTPSink(t *testing.T) {
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sink := NewHTTPSink(srv.URL)
	if err := sink.Write(context.Background(), Message{Body: []byte("payload")}); err != nil {
		t.Fatal(err)
	}
	if string(received) != "payload" {
		t.Errorf("received = %q", received)
	}
}

func TestHTTPSource(t *testing.T) {
	src := NewHTTPSource("/in")
	srv := httptest.NewServer(src.Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/in", "application/octet-stream", bytes.NewReader([]byte("msg")))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	m, err := src.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(m.Body) != "msg" {
		t.Errorf("body = %q", m.Body)
	}
}

func TestInterflow(t *testing.T) {
	f := NewInterflow(1)
	sink := f.Sink()
	src := f.Source()

	if err := sink.Write(context.Background(), Message{ID: "x", Body: []byte("y")}); err != nil {
		t.Fatal(err)
	}
	m, err := src.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "x" || string(m.Body) != "y" {
		t.Errorf("m = %+v", m)
	}
}

func TestSMTPMessageFormatting(t *testing.T) {
	msg := buildMail("sender@example.com", []string{"a@example.com", "b@example.com"}, []byte("body"))
	s := string(msg)
	for _, want := range []string{"From: sender@example.com", "To: a@example.com, b@example.com", "Subject:", "body"} {
		if !bytes.Contains([]byte(s), []byte(want)) {
			t.Errorf("message missing %q:\n%s", want, s)
		}
	}

	// Injected send captures the formatted message.
	var captured []byte
	sink := NewSMTPSink("localhost:25", "from@example.com", []string{"to@example.com"})
	sink.send = func(_ string, _ smtp.Auth, _ string, _ []string, msg []byte) error {
		captured = msg
		return nil
	}
	if err := sink.Write(context.Background(), Message{Body: []byte("mail-body")}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(captured, []byte("mail-body")) {
		t.Errorf("captured = %q", captured)
	}
}

func TestWebServiceSOAPAction(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("SOAPAction")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sink := NewWebServiceSink(srv.URL, "urn:test")
	if err := sink.Write(context.Background(), Message{Body: []byte("<soap/>")}); err != nil {
		t.Fatal(err)
	}
	if got != "urn:test" {
		t.Errorf("SOAPAction = %q", got)
	}
}

func TestDocumentSink(t *testing.T) {
	dir := t.TempDir()
	sink := NewDocumentSink(dir, "ID={{id}}\nBODY={{body}}\n")
	if err := sink.Write(context.Background(), Message{ID: "doc1", Body: []byte("content")}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "doc1.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "ID=doc1\nBODY=content\n" {
		t.Errorf("rendered = %q", b)
	}
}

func TestDBSinkAndSource(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer func() { _ = db.Close() }()

	sink, err := NewDBSink(db, "messages")
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Write(context.Background(), Message{ID: "m1", Body: []byte("b1")}); err != nil {
		t.Fatal(err)
	}

	src := NewDBSource(db, "SELECT id, body FROM messages ORDER BY id")
	m, err := src.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "m1" || string(m.Body) != "b1" {
		t.Errorf("m = %+v", m)
	}
	if _, err := src.Read(context.Background()); err == nil {
		t.Error("expected exhaustion")
	}
}

func TestEnterpriseStubs(t *testing.T) {
	if err := (BrokerSink{}).Write(context.Background(), Message{}); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("broker sink = %v", err)
	}
	if _, err := (DICOMSource{}).Read(context.Background()); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("dicom source = %v", err)
	}
}
