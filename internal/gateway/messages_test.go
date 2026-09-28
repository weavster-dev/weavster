package gateway

import (
	"context"
	"errors"
	"fmt"
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

// Count reports 7 matches, or fails for flowId=count-fails.
func (messageOps) Count(_ context.Context, q MessageQuery) (int, error) {
	if q.FlowID == "count-fails" {
		return 0, errors.New("count failed")
	}
	return 7, nil
}

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

func (messageOps) Export(_ context.Context, q MessageQuery, key []byte) ([]byte, int, error) {
	return []byte(fmt.Sprintf("archive:%d:%t", q.Limit, key != nil)), 2, nil
}

func (messageOps) Import(_ context.Context, archive []byte, opts MessageImport) (MessageImportResult, error) {
	switch string(archive) {
	case "bad":
		return MessageImportResult{}, ErrInvalidArchive
	case "unknown-flow":
		return MessageImportResult{}, fmt.Errorf("%w: flowId zz", ErrFlowNotFound)
	case "fails":
		return MessageImportResult{Imported: 4}, ErrMessageImportIncomplete
	}
	return MessageImportResult{Imported: 1}, nil
}

// DeleteMatching removes 3 messages and finds 1 busy; status "fail" fails.
func (messageOps) DeleteMatching(_ context.Context, q MessageQuery) (int, int, error) {
	if q.Status == "fail" {
		return 0, 0, errors.New("disk")
	}
	return 3, 1, nil
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
		{"from after to", http.MethodGet, "/api/v1/messages?from=2026-09-27T00:00:00Z&to=2026-09-26T00:00:00Z", cfg, http.StatusBadRequest, "from must not be after to"},
		{"unencoded plus in offset", http.MethodGet, "/api/v1/messages?from=2026-09-26T12:00:00+02:00", cfg, http.StatusOK, "[]"},
		{"bad limit", http.MethodGet, "/api/v1/messages?limit=5000", cfg, http.StatusBadRequest, "between 1 and 1000"},
		{"bad offset", http.MethodGet, "/api/v1/messages?offset=-1", cfg, http.StatusBadRequest, "0 or more"},
		{"bad sort", http.MethodGet, "/api/v1/messages?sort=status", cfg, http.StatusBadRequest, "sort must be"},
		{"id range", http.MethodGet, "/api/v1/messages?idFrom=a&idTo=b", cfg, http.StatusOK, "[]"},
		{"id range reversed", http.MethodGet, "/api/v1/messages?idFrom=b&idTo=a", cfg, http.StatusBadRequest, "idFrom must not be after idTo"},
		{"bad minAttempts", http.MethodGet, "/api/v1/messages?minAttempts=0", cfg, http.StatusBadRequest, "minAttempts must be between 1 and 1000"},
		{"bad maxAttempts", http.MethodGet, "/api/v1/messages?maxAttempts=x", cfg, http.StatusBadRequest, "maxAttempts must be between 1 and 1000"},
		{"attempts reversed", http.MethodGet, "/api/v1/messages?minAttempts=3&maxAttempts=2", cfg, http.StatusBadRequest, "minAttempts must not be more than maxAttempts"},
		{"metadata without a key", http.MethodGet, "/api/v1/messages?metadata.=x", cfg, http.StatusBadRequest, "must name a metadata key and be given once"},
		{"metadata twice", http.MethodGet, "/api/v1/messages?metadata.a=1&metadata.a=2", cfg, http.StatusBadRequest, "must name a metadata key and be given once"},
		{"too many metadata filters", http.MethodGet, "/api/v1/messages?" + manyMetadata(11), cfg, http.StatusBadRequest, "at most 10 metadata filters"},
		{"count fails: the page without a total", http.MethodGet, "/api/v1/messages?flowId=count-fails", cfg, http.StatusOK, "[]"},
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
		{"export takes the whole archive limit by default", http.MethodGet, "/api/v1/messages/export", cfg, http.StatusOK, "archive:10000:false"},
		{"export with a limit", http.MethodGet, "/api/v1/messages/export?limit=50", cfg, http.StatusOK, "archive:50:false"},
		{"export limit too high", http.MethodGet, "/api/v1/messages/export?limit=10001", cfg, http.StatusBadRequest, "between 1 and 10000"},
		{"export unavailable", http.MethodGet, "/api/v1/messages/export", Config{}, http.StatusServiceUnavailable, "messages unavailable"},
		{"import unavailable", http.MethodPost, "/api/v1/messages/import", Config{}, http.StatusServiceUnavailable, "messages unavailable"},
		{"import bad overwrite", http.MethodPost, "/api/v1/messages/import?overwrite=x", cfg, http.StatusBadRequest, "overwrite must be"},
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
	// The total is in X-Total-Count.
	rec := httptest.NewRecorder()
	New(cfg).Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/messages", nil))
	if rec.Header().Get("X-Total-Count") != "7" {
		t.Errorf("X-Total-Count = %q, want 7", rec.Header().Get("X-Total-Count"))
	}
	rec = httptest.NewRecorder()
	New(cfg).Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/messages?flowId=count-fails", nil))
	if _, ok := rec.Header()["X-Total-Count"]; ok {
		t.Error("X-Total-Count set although the count failed")
	}
	// The same invalid parameter is named every time.
	for range 5 {
		rec = httptest.NewRecorder()
		New(cfg).Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/messages?metadata.b=1&metadata.b=2&metadata.=x", nil))
		if !strings.Contains(rec.Body.String(), "metadata. must name") {
			t.Fatalf("named %s, want metadata. (the first in order)", rec.Body.String())
		}
	}
	// The new filters reach the store.
	New(cfg).Router().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet,
		"/api/v1/messages?idFrom=a&idTo=m&contentType=hl7v2&minAttempts=2&maxAttempts=5&metadata.source.file=a.hl7&metadata.region=eu", nil))
	if got.IDFrom != "a" || got.IDTo != "m" || got.ContentType != "hl7v2" || got.MinAttempts != 2 || got.MaxAttempts != 5 ||
		len(got.Metadata) != 2 || got.Metadata["source.file"] != "a.hl7" || got.Metadata["region"] != "eu" {
		t.Errorf("query = %+v", got)
	}
	// The filters reach the store.
	New(cfg).Router().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet,
		"/api/v1/messages?flowId=f&status=sent&from=2026-09-26T00:00:00Z&limit=5&offset=10&sort=id", nil))
	want := MessageQuery{FlowID: "f", Status: "sent", From: time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC), Limit: 5, Offset: 10, Sort: "id"}
	if !got.From.Equal(want.From) || got.FlowID != want.FlowID || got.Status != want.Status || got.Limit != 5 || got.Offset != 10 || got.Sort != "id" {
		t.Errorf("query = %+v", got)
	}
}

func TestMessageArchiveHandlers(t *testing.T) {
	cfg := Config{Messages: queryRecorder{got: &MessageQuery{}}}
	key := "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=" // base64 of 32 bytes
	tests := []struct {
		name, method, path, body, key string
		status                        int
		want                          string
	}{
		{"import", http.MethodPost, "/api/v1/messages/import?flowId=f&overwrite=true", "ok", key, http.StatusOK, `"imported":1`},
		{"import bad archive", http.MethodPost, "/api/v1/messages/import", "bad", "", http.StatusBadRequest, "invalid message archive"},
		{"import unknown flow", http.MethodPost, "/api/v1/messages/import?flowId=zz", "unknown-flow", "", http.StatusNotFound, "flowId zz"},
		{"import incomplete", http.MethodPost, "/api/v1/messages/import", "fails", "", http.StatusInternalServerError, `"imported":4`},
		{"bad key", http.MethodPost, "/api/v1/messages/import", "ok", "c2hvcnQ=", http.StatusBadRequest, "base64 of 32 bytes"},
		{"bad key on export", http.MethodGet, "/api/v1/messages/export", "", "!!", http.StatusBadRequest, "base64 of 32 bytes"},
		{"export with key", http.MethodGet, "/api/v1/messages/export", "", key, http.StatusOK, "archive:10000:true"},
		{"import too large", http.MethodPost, "/api/v1/messages/import", strings.Repeat("x", maxArchiveBytes+1), "", http.StatusRequestEntityTooLarge, "larger than 100 MiB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.key != "" {
				req.Header.Set(archiveKeyHeader, tt.key)
			}
			rec := httptest.NewRecorder()
			New(cfg).Router().ServeHTTP(rec, req)
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("got %d %q; want %d containing %q", rec.Code, rec.Body.String(), tt.status, tt.want)
			}
		})
	}
}

// recordingLifecycle logs transitions; failOn makes one action fail.
type recordingLifecycle struct {
	fakeLifecycle
	log    *[]string
	failOn string
}

func (l recordingLifecycle) Transition(_ context.Context, id, action string) (Flow, error) {
	*l.log = append(*l.log, action+" "+id)
	if id+" "+action == l.failOn {
		return Flow{}, fmt.Errorf("%w: cannot %s", ErrInvalidTransition, action)
	}
	return Flow{ID: id}, nil
}

func TestMessagesBulkDelete(t *testing.T) {
	flows := &fakeFlows{flows: []Flow{{ID: "a", Status: "started"}, {ID: "b", Status: "stopped"}, {ID: "c", Status: "started"}}}
	tests := []struct {
		name, path, failOn string
		flows              FlowStore
		status             int
		body               string
		log                string
	}{
		{"by flow", "/api/v1/messages?flowId=a", "", flows, http.StatusOK, `{"deleted":3,"busy":1,"restarted":[]}`, ""},
		{"by status", "/api/v1/messages?status=errored", "", flows, http.StatusOK, `"deleted":3`, ""},
		{"by metadata", "/api/v1/messages?metadata.source.file=a.hl7", "", flows, http.StatusOK, `"deleted":3`, ""},
		{"by attempts", "/api/v1/messages?minAttempts=5", "", flows, http.StatusOK, `"deleted":3`, ""},
		{"no filter", "/api/v1/messages", "", flows, http.StatusBadRequest, "all=true", ""},
		{"all", "/api/v1/messages?all=true", "", flows, http.StatusOK, `"deleted":3`, ""},
		{"all false is no filter", "/api/v1/messages?all=false", "", flows, http.StatusBadRequest, "all=true", ""},
		{"bad all", "/api/v1/messages?all=yes", "", flows, http.StatusBadRequest, "all must be true or false", ""},
		{"limit refused", "/api/v1/messages?all=true&limit=5", "", flows, http.StatusBadRequest, "limit does not apply", ""},
		{"bad from", "/api/v1/messages?from=today", "", flows, http.StatusBadRequest, "RFC 3339", ""},
		{"restart all", "/api/v1/messages?all=true&restart=true", "", flows, http.StatusOK, `"restarted":["a","c"]`, "stop a,stop c,start a,start c"},
		{"restart one flow", "/api/v1/messages?flowId=c&restart=true", "", flows, http.StatusOK, `"restarted":["c"]`, "stop c,start c"},
		{"stop fails", "/api/v1/messages?all=true&restart=true", "c stop", flows, http.StatusConflict, "cannot stop", "stop a,stop c,start a"},
		{"start fails", "/api/v1/messages?all=true&restart=true", "a start", flows, http.StatusInternalServerError, "still stopped; start them with POST /api/v1/flows/{id}/start: a", "stop a,stop c,start a,start c"},
		{"delete fails", "/api/v1/messages?status=fail&restart=true", "", flows, http.StatusInternalServerError, "removal stopped after 0 messages", "stop a,stop c,start a,start c"},
		{"delete and start fail", "/api/v1/messages?status=fail&restart=true", "c start", flows, http.StatusInternalServerError, "still stopped; start them with POST /api/v1/flows/{id}/start: c", "stop a,stop c,start a,start c"},
		{"both flags bad", "/api/v1/messages?all=yes&restart=maybe", "", flows, http.StatusBadRequest, "all must be true or false", ""},
		{"list fails", "/api/v1/messages?all=true&restart=true", "", &errFlows{}, http.StatusInternalServerError, "internal error", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var log []string
			cfg := Config{Messages: queryRecorder{got: &MessageQuery{}}, Flows: tt.flows, Lifecycle: recordingLifecycle{log: &log, failOn: tt.failOn}}
			rec := httptest.NewRecorder()
			New(cfg).Router().ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, tt.path, nil))
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.body) {
				t.Errorf("got %d %q; want %d containing %q", rec.Code, rec.Body.String(), tt.status, tt.body)
			}
			if got := strings.Join(log, ","); got != tt.log {
				t.Errorf("transitions = %q, want %q", got, tt.log)
			}
		})
	}
	for _, cfg := range []Config{{}, {Messages: queryRecorder{got: &MessageQuery{}}}} {
		rec := httptest.NewRecorder()
		New(cfg).Router().ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/messages?all=true&restart=true", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("unavailable: %d %s", rec.Code, rec.Body.String())
		}
	}
}

// manyMetadata is n distinct metadata filters as a query string.
func manyMetadata(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf("metadata.k%d=v", i)
	}
	return strings.Join(parts, "&")
}
