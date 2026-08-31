package adapters

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type errorReadCloser struct{ err error }

func (r errorReadCloser) Read([]byte) (int, error) { return 0, r.err }
func (errorReadCloser) Close() error               { return nil }

func TestHTTPSourceHandlerRejectsUnreadableBody(t *testing.T) {
	src := NewHTTPSource("/messages")
	defer func() { _ = src.Close() }()

	wantErr := errors.New("request body unavailable")
	req := httptest.NewRequest(http.MethodPost, "/messages", nil)
	req.Body = errorReadCloser{err: wantErr}
	rec := httptest.NewRecorder()

	src.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), wantErr.Error()) {
		t.Errorf("body = %q, want error %q", rec.Body.String(), wantErr)
	}
}

func TestHTTPSourceHandlerRejectsCancelledRequestWhenBufferFull(t *testing.T) {
	src := NewHTTPSource("/messages")
	defer func() { _ = src.Close() }()
	for range cap(src.ch) {
		src.ch <- Message{}
	}

	req := httptest.NewRequest(http.MethodPost, "/messages", nil)
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	src.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
