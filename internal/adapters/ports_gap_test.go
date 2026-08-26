package adapters

import (
	"context"
	"errors"
	"testing"
)

// TestBrokerSourceRead and TestDICOMSinkWrite close the remaining gaps in the
// Enterprise adapter stubs: BrokerSource.Read and DICOMSink.Write were the
// only Read/Write stub methods not yet exercised by adapters_test.go (which
// covers BrokerSink.Write and DICOMSource.Read). Without these, a change that
// accidentally implemented real behavior instead of returning
// ErrNotImplemented would go undetected.
func TestBrokerSourceRead(t *testing.T) {
	_, err := (BrokerSource{}).Read(context.Background())
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("BrokerSource.Read() error = %v, want ErrNotImplemented", err)
	}
}

func TestDICOMSinkWrite(t *testing.T) {
	err := (DICOMSink{}).Write(context.Background(), Message{})
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("DICOMSink.Write() error = %v, want ErrNotImplemented", err)
	}
}
