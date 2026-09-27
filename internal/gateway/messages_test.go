package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// messageOps answers the message operations other than Search: message
// "m" exists (content "hello"), "busy" is being processed, others are
// unknown.
type messageOps struct{}

func (messageOps) Get(_ context.Context, id string) (Message, error) {
	if id != "m" {
		return Message{}, ErrMessageNotFound
	}
	return Message{ID: "m", FlowID: "f", Status: "sent"}, nil
}

func (messageOps) Content(_ context.Context, id, part string) (MessageContent, error) {
	if id != "m" {
		return MessageContent{}, ErrMessageNotFound
	}
	return MessageContent{Body: []byte("hello " + part), ContentType: "application/octet-stream"}, nil
}

func (messageOps) Delete(_ context.Context, id string) error {
	switch id {
	case "m":
		return nil
	case "busy":
		return ErrMessageBusy
	}
	return ErrMessageNotFound
}

func (messageOps) Reprocess(_ context.Context, id string) (IngestResult, error) {
	if id != "m" {
		return IngestResult{}, ErrMessageNotFound
	}
	return IngestResult{ID: "m2", Status: "sent"}, nil
}

// queryRecorder keeps the last search query.
type queryRecorder struct {
	messageOps
	got *MessageQuery
}

func (r queryRecorder) Search(_ context.Context, q MessageQuery) ([]Message, error) {
	*r.got = q
	return []Message{}, nil
}

func TestMessageHandlers(t *testing.T) {
	var got MessageQuery
	cfg := Config{Messages: queryRecorder{got: &got}}
	tests := []struct {
		name, method, path string
		cfg                Config
		status             int
		body               string
	}{
		{"search defaults", http.MethodGet, "/api/v1/messages", cfg, http.StatusOK, "[]"},
		{"search filters", http.MethodGet, "/api/v1/messages?flowId=f&status=sent&from=2026-09-26T00:00:00Z&to=2026-09-27T00:00:00Z&limit=5&offset=10&sort=id", cfg, http.StatusOK, "[]"},
		{"bad from", http.MethodGet, "/api/v1/messages?from=yesterday", cfg, http.StatusBadRequest, "RFC 3339"},
		{"unencoded plus in offset", http.MethodGet, "/api/v1/messages?from=2026-09-26T12:00:00+02:00", cfg, http.StatusOK, "[]"},
		{"bad limit", http.MethodGet, "/api/v1/messages?limit=5000", cfg, http.StatusBadRequest, "between 1 and 1000"},
		{"bad offset", http.MethodGet, "/api/v1/messages?offset=-1", cfg, http.StatusBadRequest, "0 or more"},
		{"bad sort", http.MethodGet, "/api/v1/messages?sort=status", cfg, http.StatusBadRequest, "sort must be"},
		{"get", http.MethodGet, "/api/v1/messages/m", cfg, http.StatusOK, `"id":"m"`},
		{"get unknown", http.MethodGet, "/api/v1/messages/zz", cfg, http.StatusNotFound, "message not found"},
		{"content", http.MethodGet, "/api/v1/messages/m/content?part=transformed", cfg, http.StatusOK, "hello transformed"},
		{"content default part", http.MethodGet, "/api/v1/messages/m/content", cfg, http.StatusOK, "hello raw"},
		{"content bad part", http.MethodGet, "/api/v1/messages/m/content?part=encoded", cfg, http.StatusBadRequest, "raw or transformed"},
		{"content unknown", http.MethodGet, "/api/v1/messages/zz/content", cfg, http.StatusNotFound, "message not found"},
		{"delete", http.MethodDelete, "/api/v1/messages/m", cfg, http.StatusNoContent, ""},
		{"delete busy", http.MethodDelete, "/api/v1/messages/busy", cfg, http.StatusConflict, "being processed"},
		{"reprocess", http.MethodPost, "/api/v1/messages/m/reprocess", cfg, http.StatusAccepted, `"id":"m2"`},
		{"reprocess unknown", http.MethodPost, "/api/v1/messages/zz/reprocess", cfg, http.StatusNotFound, "message not found"},
		{"get unavailable", http.MethodGet, "/api/v1/messages/m", Config{}, http.StatusServiceUnavailable, "messages unavailable"},
		{"content unavailable", http.MethodGet, "/api/v1/messages/m/content", Config{}, http.StatusServiceUnavailable, "messages unavailable"},
		{"delete unavailable", http.MethodDelete, "/api/v1/messages/m", Config{}, http.StatusServiceUnavailable, "messages unavailable"},
		{"reprocess unavailable", http.MethodPost, "/api/v1/messages/m/reprocess", Config{}, http.StatusServiceUnavailable, "messages unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(tt.cfg).Router().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.body) {
				t.Errorf("got %d %q; want %d containing %q", rec.Code, rec.Body.String(), tt.status, tt.body)
			}
		})
	}
	// The filters reach the store.
	New(cfg).Router().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet,
		"/api/v1/messages?flowId=f&status=sent&from=2026-09-26T00:00:00Z&limit=5&offset=10&sort=id", nil))
	want := MessageQuery{FlowID: "f", Status: "sent", From: time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC), Limit: 5, Offset: 10, Sort: "id"}
	if !got.From.Equal(want.From) || got.FlowID != want.FlowID || got.Status != want.Status || got.Limit != 5 || got.Offset != 10 || got.Sort != "id" {
		t.Errorf("query = %+v", got)
	}
}
