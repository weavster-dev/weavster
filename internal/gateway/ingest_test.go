package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeIngest struct{ err error }

func (f fakeIngest) Ingest(_ context.Context, flowID string, body []byte) (IngestResult, error) {
	if f.err != nil {
		return IngestResult{}, f.err
	}
	return IngestResult{ID: "m-" + flowID, Status: "sent"}, nil
}

func TestIngestHandler(t *testing.T) {
	tests := []struct {
		name   string
		ingest MessageIngester
		body   string
		want   int
		bodyIn string
	}{
		{"accepted", fakeIngest{}, `{}`, http.StatusAccepted, `"id":"m-f"`},
		{"unavailable", nil, `{}`, http.StatusServiceUnavailable, "unavailable"},
		{"unknown flow", fakeIngest{err: ErrFlowNotFound}, `{}`, http.StatusNotFound, "flow not found"},
		{"invalid message", fakeIngest{err: fmt.Errorf("%w: body must be a JSON object", ErrInvalidMessage)}, `x`, http.StatusBadRequest, "body must be a JSON object"},
		{"internal", fakeIngest{err: errors.New("disk full")}, `{}`, http.StatusInternalServerError, "internal error"},
		{"too large", fakeIngest{}, strings.Repeat("x", maxMessageBytes+1), http.StatusRequestEntityTooLarge, "too large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := New(Config{Ingest: tt.ingest}).Router()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/flows/f/messages", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			if rec.Code != tt.want || !strings.Contains(rec.Body.String(), tt.bodyIn) {
				t.Errorf("got %d %q, want %d containing %q", rec.Code, rec.Body.String(), tt.want, tt.bodyIn)
			}
		})
	}
}
