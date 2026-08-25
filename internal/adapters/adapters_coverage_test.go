package adapters

import (
	"context"
	"net"
	"testing"
)

// TestAdapterNameAndClose exercises the Name() and Close() methods of every
// Source/Sink adapter. These trivial methods were previously untested
// (0% coverage), even though they are part of the exported port contracts
// exercised by the gateway/router at runtime.
func TestAdapterNameAndClose(t *testing.T) {
	dir := t.TempDir()

	fileSrc, err := NewFileSource(dir, "*")
	if err != nil {
		t.Fatal(err)
	}
	fileSink := NewFileSink(dir)
	httpSink := NewHTTPSink("http://example.invalid")
	httpSrc := NewHTTPSource("/in")
	flow := NewInterflow(1)
	smtpSink := NewSMTPSink("localhost:25", "from@example.com", []string{"to@example.com"})
	webSink := NewWebServiceSink("http://example.invalid", "urn:test")
	docSink := NewDocumentSink(dir, "{{body}}")
	mllpSink := NewMLLPSink("127.0.0.1:0")

	cases := []struct {
		name     string
		wantName string
		closer   interface{ Close() error }
		namer    interface{ Name() string }
	}{
		{"FileSource", "file", fileSrc, fileSrc},
		{"FileSink", "file", fileSink, fileSink},
		{"HTTPSink", "http", httpSink, httpSink},
		{"HTTPSource", "http", httpSrc, httpSrc},
		{"InterflowSource", "in-memory", flow.Source(), flow.Source()},
		{"SMTPSink", "smtp", smtpSink, smtpSink},
		{"WebServiceSink", "web-service", webSink, webSink},
		{"DocumentSink", "document", docSink, docSink},
		{"MLLPSink", "tcp", mllpSink, mllpSink},
		{"BrokerSource", "broker", BrokerSource{}, BrokerSource{}},
		{"BrokerSink", "broker", BrokerSink{}, BrokerSink{}},
		{"DICOMSource", "dicom", DICOMSource{}, DICOMSource{}},
		{"DICOMSink", "dicom", DICOMSink{}, DICOMSink{}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.namer.Name(); got != c.wantName {
				t.Errorf("Name() = %q, want %q", got, c.wantName)
			}
			if err := c.closer.Close(); err != nil {
				t.Errorf("Close() = %v, want nil", err)
			}
		})
	}

	// Interflow sink Close() is exercised separately since closing it closes
	// the underlying channel shared with the source created above.
	sink := flow.Sink()
	if got := sink.Name(); got != "in-memory" {
		t.Errorf("interflow sink Name() = %q", got)
	}
	if err := sink.Close(); err != nil {
		t.Errorf("interflow sink Close() = %v", err)
	}

	// DBSink/DBSource Name() and Close() don't require a live DB handle for
	// these no-op/constant methods.
	dbSink := &DBSink{}
	if got := dbSink.Name(); got != "database" {
		t.Errorf("DBSink Name() = %q", got)
	}
	if err := dbSink.Close(); err != nil {
		t.Errorf("DBSink Close() = %v", err)
	}
	dbSrc := &DBSource{}
	if got := dbSrc.Name(); got != "database" {
		t.Errorf("DBSource Name() = %q", got)
	}
	if err := dbSrc.Close(); err != nil {
		t.Errorf("DBSource Close() = %v", err)
	}
}

// TestMLLPSourceListenReadCloseLifecycle covers ListenMLLP, Addr, Name, Read
// and Close for MLLPSource, which previously had 0% coverage despite being
// the TCP listener used to accept inbound HL7 MLLP connections in
// production (spec §8 TCP MLLP source).
func TestMLLPSourceListenReadCloseLifecycle(t *testing.T) {
	src, err := ListenMLLP("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = src.Close() }()

	if src.Name() != "tcp" {
		t.Errorf("Name() = %q, want tcp", src.Name())
	}
	if src.Addr() == nil {
		t.Fatal("Addr() = nil")
	}

	addr := src.Addr().String()
	go func() {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = conn.Write(frameMLLP([]byte("ADT^A01")))
	}()

	m, err := src.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if string(m.Body) != "ADT^A01" {
		t.Errorf("Read() body = %q, want ADT^A01", m.Body)
	}

	if err := src.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}
}

// TestListenMLLPBadAddr covers the ListenMLLP error path.
func TestListenMLLPBadAddr(t *testing.T) {
	if _, err := ListenMLLP("bad-address-not-a-host:port:extra"); err == nil {
		t.Fatal("expected error for invalid address")
	}
}

// TestHTTPStatusError covers the httpStatusError.Error() method, previously
// untested, which surfaces non-2xx responses from HTTPSink/WebServiceSink.
func TestHTTPStatusError(t *testing.T) {
	err := &httpStatusError{code: 500}
	if err.Error() == "" {
		t.Error("Error() returned empty string")
	}
}
