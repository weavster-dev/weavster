package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/codecs"
	"github.com/weavster-dev/weavster/internal/gateway"
)

// TestSupportMatrixWired proves every "Implemented (wired)" server/API row in
// docs/support-matrix.md against the composed server.
func TestSupportMatrixWired(t *testing.T) {
	handler, err := buildServer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(handler)
	defer ts.Close()

	tests := []struct {
		name     string
		method   string
		path     string
		body     string
		marker   bool
		want     int
		contains string
		header   string
	}{
		{name: "openapi", method: http.MethodGet, path: "/api/openapi.yaml", want: http.StatusOK, contains: "openapi:"},
		{name: "system", method: http.MethodGet, path: "/api/v1/system", marker: true, want: http.StatusOK},
		{name: "csrf-marker", method: http.MethodGet, path: "/api/v1/system", want: http.StatusBadRequest},
		{name: "trace-blocked", method: http.MethodTrace, path: "/api/v1/system", marker: true, want: http.StatusMethodNotAllowed},
		{name: "security-headers", method: http.MethodGet, path: "/api/v1/system", marker: true, want: http.StatusOK, header: "X-Frame-Options"},
		{name: "flows-list", method: http.MethodGet, path: "/api/v1/flows", marker: true, want: http.StatusOK, contains: `"admit"`},
		{name: "flows-create", method: http.MethodPost, path: "/api/v1/flows", marker: true, body: `{"id":"lab","name":"Lab Results"}`, want: http.StatusCreated},
		{name: "flows-get", method: http.MethodGet, path: "/api/v1/flows/lab", marker: true, want: http.StatusOK, contains: "Lab Results"},
		{name: "topology-overview", method: http.MethodGet, path: "/api/v1/topology", marker: true, want: http.StatusOK, contains: "flow:lab"},
		{name: "topology-flow", method: http.MethodGet, path: "/api/v1/topology/flows/admit", marker: true, want: http.StatusOK, contains: "source:"},
		{name: "flows-delete", method: http.MethodDelete, path: "/api/v1/flows/lab", marker: true, want: http.StatusNoContent},
		{name: "messages", method: http.MethodGet, path: "/api/v1/messages", marker: true, want: http.StatusOK, contains: "[]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, ts.URL+tt.path, strings.NewReader(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			if tt.marker {
				req.Header.Set(gateway.MarkerHeader, gateway.MarkerValue)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != tt.want {
				t.Fatalf("status = %d, want %d (body %q)", resp.StatusCode, tt.want, body)
			}
			if tt.contains != "" && !strings.Contains(string(body), tt.contains) {
				t.Errorf("body %q does not contain %q", body, tt.contains)
			}
			if tt.header != "" && resp.Header.Get(tt.header) == "" {
				t.Errorf("missing header %s", tt.header)
			}
		})
	}
}

// TestSupportMatrixCodecs keeps the docs codec table in sync with
// codecs.CoverageMatrix(), the single source of codec coverage.
func TestSupportMatrixCodecs(t *testing.T) {
	data, err := os.ReadFile("../../docs/support-matrix.md")
	if err != nil {
		t.Fatal(err)
	}
	_, table, ok := strings.Cut(string(data), "<!-- codec-table:")
	if !ok {
		t.Fatal("codec-table marker not found in docs/support-matrix.md")
	}
	table, _, _ = strings.Cut(table, "\n\n")

	var documented []string
	for _, line := range strings.Split(table, "\n") {
		cells := strings.Split(line, "|")
		if len(cells) < 3 || !strings.HasPrefix(strings.TrimSpace(cells[1]), "`") {
			continue
		}
		name := strings.Trim(strings.TrimSpace(cells[1]), "`")
		enterprise := strings.TrimSpace(cells[2]) == "Enterprise-deferred"
		documented = append(documented, name+":"+boolStr(enterprise))
	}

	var want []string
	for _, e := range codecs.CoverageMatrix() {
		want = append(want, e.Name+":"+boolStr(e.Enterprise))
	}
	sort.Strings(documented)
	sort.Strings(want)
	if strings.Join(documented, ",") != strings.Join(want, ",") {
		t.Errorf("docs codec table = %v, CoverageMatrix = %v", documented, want)
	}
}

func boolStr(b bool) string {
	if b {
		return "enterprise"
	}
	return "mvp"
}
