package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeSourceIngest records the metadata it was given and replies with res, err.
type fakeSourceIngest struct {
	res  IngestResult
	err  error
	meta map[string]string
}

func (f *fakeSourceIngest) IngestFrom(_ context.Context, _ string, _ []byte, metadata map[string]string) (IngestResult, error) {
	f.meta = metadata
	return f.res, f.err
}

func TestSourceHandler(t *testing.T) {
	sent := IngestResult{ID: "m1", Status: "sent"}
	for _, tt := range []struct {
		name         string
		src          FlowSource
		method, path string
		body         io.Reader
		ingest       *fakeSourceIngest
		want         int
		bodyIn       string
	}{
		{"defaults", FlowSource{Type: "http"}, http.MethodPost, "/", strings.NewReader("{}"), &fakeSourceIngest{res: sent}, http.StatusAccepted, `"id":"m1"`},
		{"configured", FlowSource{Type: "http", Path: "/adt", Method: http.MethodPut}, http.MethodPut, "/adt", strings.NewReader("{}"), &fakeSourceIngest{res: sent}, http.StatusAccepted, `"status":"sent"`},
		{"other path", FlowSource{Type: "http", Path: "/adt"}, http.MethodPost, "/adt/x", strings.NewReader("{}"), &fakeSourceIngest{}, http.StatusNotFound, "no source at this path"},
		{"other method", FlowSource{Type: "http"}, http.MethodGet, "/", nil, &fakeSourceIngest{}, http.StatusMethodNotAllowed, "use POST"},
		{"too large", FlowSource{Type: "http"}, http.MethodPost, "/", strings.NewReader(strings.Repeat("x", MaxMessageBytes+1)), &fakeSourceIngest{}, http.StatusRequestEntityTooLarge, "larger than 10 MiB"},
		{"unreadable", FlowSource{Type: "http"}, http.MethodPost, "/", errReader{}, &fakeSourceIngest{}, http.StatusBadRequest, "could not read"},
		{"stopped", FlowSource{Type: "http"}, http.MethodPost, "/", strings.NewReader("{}"), &fakeSourceIngest{err: fmt.Errorf("%w: flow f is stopped", ErrFlowNotRunning)}, http.StatusServiceUnavailable, "not accepting messages"},
		{"removed", FlowSource{Type: "http"}, http.MethodPost, "/", strings.NewReader("{}"), &fakeSourceIngest{err: ErrFlowNotFound}, http.StatusServiceUnavailable, "not accepting messages"},
		{"refused", FlowSource{Type: "http"}, http.MethodPost, "/", strings.NewReader("x"), &fakeSourceIngest{err: fmt.Errorf("%w: body must be a JSON object", ErrInvalidMessage)}, http.StatusBadRequest, "body must be a JSON object"},
		{"stored, then failed", FlowSource{Type: "http"}, http.MethodPost, "/", strings.NewReader("{}"), &fakeSourceIngest{res: IngestResult{ID: "m2"}, err: errors.New("disk full")}, http.StatusAccepted, `"id":"m2"`},
		{"failed", FlowSource{Type: "http"}, http.MethodPost, "/", strings.NewReader("{}"), &fakeSourceIngest{err: errors.New("disk full")}, http.StatusInternalServerError, "internal error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			SourceHandler("f", tt.src, "", tt.ingest).ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, tt.body))
			if rec.Code != tt.want || !strings.Contains(rec.Body.String(), tt.bodyIn) {
				t.Errorf("got %d %q, want %d containing %q", rec.Code, rec.Body.String(), tt.want, tt.bodyIn)
			}
			if tt.want == http.StatusMethodNotAllowed && rec.Header().Get("Allow") != http.MethodPost {
				t.Errorf("Allow = %q", rec.Header().Get("Allow"))
			}
			if tt.want == http.StatusAccepted && tt.ingest.meta["source.http.path"] != tt.path {
				t.Errorf("metadata = %v", tt.ingest.meta)
			}
		})
	}
}

type fakeSourcePorts []PortInUse

func (f fakeSourcePorts) Ports() []PortInUse { return f }

// TestPortsInUseListsSources: flows' open http sources follow the server's
// own listeners.
func TestPortsInUseListsSources(t *testing.T) {
	srv := New(Config{
		Listeners: []PortInUse{{Address: ":8080", Port: 8080, UsedBy: "api"}},
		Sources:   fakeSourcePorts{{Address: ":9001", Port: 9001, UsedBy: "flow:adt"}},
	}).Router()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/flows/ports-in-use", nil))
	want := `[{"address":":8080","port":8080,"usedBy":"api"},{"address":":9001","port":9001,"usedBy":"flow:adt"}]`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

// TestSourceHandlerBasicAuth: with a username, only requests with that user
// and password get through; the rest get 401 before path or method checks.
func TestSourceHandlerBasicAuth(t *testing.T) {
	h := SourceHandler("f", FlowSource{Type: "http", Path: "/adt", Username: "lab"}, "s3cret", &fakeSourceIngest{res: IngestResult{ID: "m1"}})
	for _, tt := range []struct {
		name, user, password, path string
		basic                      bool
		want                       int
	}{
		{"right", "lab", "s3cret", "/adt", true, http.StatusAccepted},
		{"wrong password", "lab", "nope", "/adt", true, http.StatusUnauthorized},
		{"wrong user", "other", "s3cret", "/adt", true, http.StatusUnauthorized},
		{"no credentials", "", "", "/adt", false, http.StatusUnauthorized},
		{"no credentials, other path", "", "", "/x", false, http.StatusUnauthorized},
		{"right, other path", "lab", "s3cret", "/x", true, http.StatusNotFound},
	} {
		req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader("{}"))
		if tt.basic {
			req.SetBasicAuth(tt.user, tt.password)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tt.want {
			t.Errorf("%s: %d %s, want %d", tt.name, rec.Code, rec.Body.String(), tt.want)
		}
		if tt.want == http.StatusUnauthorized && !strings.HasPrefix(rec.Header().Get("WWW-Authenticate"), "Basic ") {
			t.Errorf("%s: WWW-Authenticate = %q", tt.name, rec.Header().Get("WWW-Authenticate"))
		}
	}
}
