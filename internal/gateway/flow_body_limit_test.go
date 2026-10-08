package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type flowBodyStore struct {
	fakeFlows
	fakeUpdater
	writes int
}

func (s *flowBodyStore) Create(_ context.Context, f Flow) (Flow, error) {
	s.writes++
	return f, nil
}

func (s *flowBodyStore) Update(_ context.Context, _ string, f Flow, _ bool) (Flow, error) {
	s.writes++
	return f, nil
}

type flowPaddingReader struct{}

func (flowPaddingReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	return len(p), nil
}

type flowCountingReader struct {
	io.Reader
	n int64
}

func (r *flowCountingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.n += int64(n)
	return n, err
}

func TestFlowRequestBodyLimit(t *testing.T) {
	const doc = `{"id":"f","name":"flow"}`
	for _, route := range []struct {
		method, path string
		status       int
	}{
		{http.MethodPost, "/api/v1/flows", http.StatusCreated},
		{http.MethodPut, "/api/v1/flows/f", http.StatusOK},
	} {
		for _, size := range []struct {
			name string
			n    int64
		}{
			{"small", int64(len(doc))},
			{"exact limit", maxImportBytes},
			{"limit plus one", maxImportBytes + 1},
			{"oversized", maxImportBytes + (1 << 20)},
		} {
			for _, knownLength := range []bool{false, true} {
				name := route.method + "/" + size.name + "/unknown length"
				if knownLength {
					name = route.method + "/" + size.name + "/known length"
				}
				t.Run(name, func(t *testing.T) {
					store := &flowBodyStore{}
					h := New(Config{
						Flows: store, FlowUpdates: store,
						Auth: fakeAuth{users: map[string]Identity{
							"editor": {Username: "editor", Permissions: []string{"flows:edit"}},
						}},
						Authorizer: fakeAuthz{},
					}).Router()
					body := &flowCountingReader{Reader: io.MultiReader(strings.NewReader(doc),
						io.LimitReader(flowPaddingReader{}, size.n-int64(len(doc))))}
					req := httptest.NewRequest(route.method, route.path, body)
					req.ContentLength = -1
					if knownLength {
						req.ContentLength = size.n
					}
					req.SetBasicAuth("editor", "pw")
					rec := httptest.NewRecorder()
					h.ServeHTTP(rec, req)
					wantStatus, wantWrites := route.status, 1
					if size.n > maxImportBytes {
						wantStatus, wantWrites = http.StatusRequestEntityTooLarge, 0
					}
					if rec.Code != wantStatus || store.writes != wantWrites || body.n > maxImportBytes+1 {
						t.Errorf("status=%d writes=%d bytes read=%d; want status=%d writes=%d reads<=%d",
							rec.Code, store.writes, body.n, wantStatus, wantWrites, maxImportBytes+1)
					}
				})
			}
		}
	}
}
