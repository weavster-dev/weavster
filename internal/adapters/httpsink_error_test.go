package adapters

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestHTTPSinkWriteRejectsInvalidURL(t *testing.T) {
	sink := NewHTTPSink("http://example.com/\x00")

	if err := sink.Write(context.Background(), Message{Body: []byte("payload")}); err == nil {
		t.Fatal("Write() error = nil, want invalid URL error")
	}
}

func TestHTTPSinkWriteReturnsTransportError(t *testing.T) {
	wantErr := errors.New("transport failed")
	sink := NewHTTPSink("http://example.invalid")
	sink.client = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, wantErr
		}),
	}

	err := sink.Write(context.Background(), Message{Body: []byte("payload")})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Write() error = %v, want error wrapping %v", err, wantErr)
	}
}
