// Package adapters implements the Source and Sink ports with MVP adapters
// (file, http, tcp MLLP, in-memory, database, smtp, web-service, document).
package adapters

import (
	"context"
	"errors"
	"io"
)

// ErrNotImplemented is returned by Enterprise-scoped adapter stubs
// (message-queue broker, DICOM).
var ErrNotImplemented = errors.New("adapters: enterprise adapter not implemented in MVP")

// Message is the unit moved by sources and sinks.
type Message struct {
	ID       string
	Body     []byte
	Metadata map[string]string
}

// Source acquires messages (arch §3.1).
type Source interface {
	Name() string
	// Read returns the next message, or io.EOF when the source is exhausted.
	Read(ctx context.Context) (Message, error)
	Close() error
}

// Sink delivers messages (arch §3.1).
type Sink interface {
	Name() string
	Write(ctx context.Context, m Message) error
	Close() error
}

// BrokerSource is the Enterprise message-queue source stub (arch §9.2).
type BrokerSource struct{}

func (BrokerSource) Name() string { return "broker" }
func (BrokerSource) Read(context.Context) (Message, error) {
	return Message{}, ErrNotImplemented
}
func (BrokerSource) Close() error { return nil }

// BrokerSink is the Enterprise message-queue sink stub (arch §9.2).
type BrokerSink struct{}

func (BrokerSink) Name() string { return "broker" }
func (BrokerSink) Write(context.Context, Message) error {
	return ErrNotImplemented
}
func (BrokerSink) Close() error { return nil }

// DICOMSource is the Enterprise medical-imaging source stub (arch §9.2).
type DICOMSource struct{}

func (DICOMSource) Name() string { return "dicom" }
func (DICOMSource) Read(context.Context) (Message, error) {
	return Message{}, ErrNotImplemented
}
func (DICOMSource) Close() error { return nil }

// DICOMSink is the Enterprise medical-imaging sink stub (arch §9.2).
type DICOMSink struct{}

func (DICOMSink) Name() string { return "dicom" }
func (DICOMSink) Write(context.Context, Message) error {
	return ErrNotImplemented
}
func (DICOMSink) Close() error { return nil }

var (
	_ Source = BrokerSource{}
	_ Source = DICOMSource{}
	_ Sink   = BrokerSink{}
	_ Sink   = DICOMSink{}
	_        = io.EOF
)
