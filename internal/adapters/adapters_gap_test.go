package adapters

import (
	"context"
	"database/sql"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"
)

// TestAdapterNamesGap covers the Name() accessors that are otherwise never
// exercised by the existing Read/Write-focused tests.
func TestAdapterNamesGap(t *testing.T) {
	dir := t.TempDir()
	fileSrc, err := NewFileSource(dir, "*")
	if err != nil {
		t.Fatal(err)
	}
	flow := NewInterflow(1)

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	dbSink, err := NewDBSink(db, "gap_names")
	if err != nil {
		t.Fatal(err)
	}

	named := []struct {
		name string
		obj  interface{ Name() string }
		want string
	}{
		{"FileSource", fileSrc, "file"},
		{"FileSink", NewFileSink(dir), "file"},
		{"HTTPSink", NewHTTPSink("http://example.invalid"), "http"},
		{"HTTPSource", NewHTTPSource("/in"), "http"},
		{"InterflowSource", flow.Source(), "in-memory"},
		{"InterflowSink", flow.Sink(), "in-memory"},
		{"DBSink", dbSink, "database"},
		{"DBSource", NewDBSource(db, "SELECT 1"), "database"},
		{"SMTPSink", NewSMTPSink("localhost:25", "a@b.com", []string{"c@d.com"}), "smtp"},
		{"WebServiceSink", NewWebServiceSink("http://example.invalid", "Action"), "web-service"},
		{"DocumentSink", NewDocumentSink(dir, "{{body}}"), "document"},
		{"MLLPSink", NewMLLPSink("127.0.0.1:0"), "tcp"},
		{"BrokerSource", BrokerSource{}, "broker"},
		{"BrokerSink", BrokerSink{}, "broker"},
		{"DICOMSource", DICOMSource{}, "dicom"},
		{"DICOMSink", DICOMSink{}, "dicom"},
	}
	for _, tc := range named {
		if got := tc.obj.Name(); got != tc.want {
			t.Errorf("%s.Name() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestAdapterClose covers the Close() no-ops and lifecycle paths that are
// never invoked by the Read/Write-focused tests.
func TestAdapterClose(t *testing.T) {
	dir := t.TempDir()
	fileSrc, err := NewFileSource(dir, "*")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	dbSink, err := NewDBSink(db, "gap_close")
	if err != nil {
		t.Fatal(err)
	}
	dbSrc := NewDBSource(db, "SELECT 1")

	closers := []struct {
		name string
		obj  interface{ Close() error }
	}{
		{"FileSource", fileSrc},
		{"FileSink", NewFileSink(dir)},
		{"HTTPSink", NewHTTPSink("http://example.invalid")},
		{"DBSink", dbSink},
		{"DBSource (no rows opened)", dbSrc},
		{"SMTPSink", NewSMTPSink("localhost:25", "a@b.com", nil)},
		{"WebServiceSink", NewWebServiceSink("http://example.invalid", "")},
		{"DocumentSink", NewDocumentSink(dir, "")},
		{"MLLPSink (never dialed)", NewMLLPSink("127.0.0.1:0")},
		{"BrokerSource", BrokerSource{}},
		{"BrokerSink", BrokerSink{}},
		{"DICOMSource", DICOMSource{}},
		{"DICOMSink", DICOMSink{}},
	}
	for _, tc := range closers {
		if err := tc.obj.Close(); err != nil {
			t.Errorf("%s.Close() = %v, want nil", tc.name, err)
		}
	}
}

// TestDBSourceCloseAfterRead ensures Close() drains the rows cursor once
// Read has opened it.
func TestDBSourceCloseAfterRead(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	sink, err := NewDBSink(db, "gap_close_after_read")
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Write(context.Background(), Message{ID: "x", Body: []byte("y")}); err != nil {
		t.Fatal(err)
	}

	src := NewDBSource(db, "SELECT id, body FROM gap_close_after_read")
	if _, err := src.Read(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := src.Close(); err != nil {
		t.Errorf("Close() after Read = %v, want nil", err)
	}
}

// TestHTTPSourceHandlerRejectsWrongPathAndMethod covers the 404 branches of
// HTTPSource.Handler that the happy-path test never exercises.
func TestHTTPSourceHandlerRejectsWrongPathAndMethod(t *testing.T) {
	src := NewHTTPSource("/expected")
	defer func() { _ = src.Close() }()
	srv := httptest.NewServer(src.Handler())
	defer srv.Close()

	// Wrong path.
	resp, err := http.Post(srv.URL+"/other", "application/octet-stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("wrong path status = %d, want 404", resp.StatusCode)
	}

	// Wrong method.
	resp, err = http.Get(srv.URL + "/expected")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("wrong method status = %d, want 404", resp.StatusCode)
	}
}

// TestMLLPSourceLifecycle exercises ListenMLLP, Addr, Read, and Close, none
// of which are covered by the existing MLLPSink-focused test.
func TestMLLPSourceLifecycle(t *testing.T) {
	src, err := ListenMLLP("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = src.Close() }()

	if src.Name() != "tcp" {
		t.Errorf("Name() = %q, want tcp", src.Name())
	}
	addr := src.Addr()
	if addr == nil {
		t.Fatal("Addr() = nil")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := net.Dial("tcp", addr.String())
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.Close() }()
		if _, err := conn.Write(frameMLLP([]byte("hello"))); err != nil {
			t.Error(err)
		}
	}()

	m, err := src.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(m.Body) != "hello" {
		t.Errorf("m.Body = %q, want hello", m.Body)
	}
	<-done
}
