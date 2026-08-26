package adapters

import (
	"context"
	"errors"
	"testing"
)

// TestEnterpriseStubsNotImplemented covers the Enterprise-scoped
// BrokerSource/BrokerSink/DICOMSource/DICOMSink stubs, which previously had
// 0% coverage. These stubs must consistently report Name() and return
// ErrNotImplemented from Read/Write so callers can detect and handle
// unsupported adapter types (arch §9.2), and Close() must remain a safe
// no-op.
func TestEnterpriseStubsNotImplemented(t *testing.T) {
	ctx := context.Background()

	sources := []Source{BrokerSource{}, DICOMSource{}}
	wantSourceName := map[Source]string{
		sources[0]: "broker",
		sources[1]: "dicom",
	}
	for _, src := range sources {
		if got := src.Name(); got != wantSourceName[src] {
			t.Errorf("Name() = %q, want %q", got, wantSourceName[src])
		}
		msg, err := src.Read(ctx)
		if !errors.Is(err, ErrNotImplemented) {
			t.Errorf("Read() error = %v, want ErrNotImplemented", err)
		}
		if msg.ID != "" || msg.Body != nil || msg.Metadata != nil {
			t.Errorf("Read() message = %+v, want zero value", msg)
		}
		if err := src.Close(); err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}
	}

	sinks := []Sink{BrokerSink{}, DICOMSink{}}
	wantSinkName := map[Sink]string{
		sinks[0]: "broker",
		sinks[1]: "dicom",
	}
	for _, sink := range sinks {
		if got := sink.Name(); got != wantSinkName[sink] {
			t.Errorf("Name() = %q, want %q", got, wantSinkName[sink])
		}
		if err := sink.Write(ctx, Message{ID: "1"}); !errors.Is(err, ErrNotImplemented) {
			t.Errorf("Write() error = %v, want ErrNotImplemented", err)
		}
		if err := sink.Close(); err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}
	}
}
