package main

import (
	"bytes"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

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
		headers  map[string]string
	}{
		{name: "openapi", method: http.MethodGet, path: "/api/openapi.yaml", want: http.StatusOK, contains: "openapi:"},
		{name: "system", method: http.MethodGet, path: "/api/v1/system", marker: true, want: http.StatusOK},
		{name: "csrf-marker", method: http.MethodGet, path: "/api/v1/system", want: http.StatusBadRequest},
		{name: "trace-blocked", method: http.MethodTrace, path: "/api/v1/system", marker: true, want: http.StatusMethodNotAllowed},
		{name: "track-blocked", method: "TRACK", path: "/api/v1/system", marker: true, want: http.StatusMethodNotAllowed},
		{name: "security-headers", method: http.MethodGet, path: "/api/v1/system", marker: true, want: http.StatusOK, headers: map[string]string{
			"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
			"X-Frame-Options":           "DENY",
			"Content-Security-Policy":   "frame-ancestors 'none'",
			"X-Content-Type-Options":    "nosniff",
		}},
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
			for h, want := range tt.headers {
				if got := resp.Header.Get(h); got != want {
					t.Errorf("header %s = %q, want %q", h, got, want)
				}
			}
		})
	}
}

// TestSupportMatrixCLI proves the wired CLI rows: -a/-s batch mode against
// the composed server, and the no-subcommand default starting the server.
func TestSupportMatrixCLI(t *testing.T) {
	handler, err := buildServer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(handler)
	defer ts.Close()

	tests := []struct {
		name     string
		script   string
		want     int
		contains string
	}{
		{name: "batch-status", script: "status\n", want: 0, contains: `"weavster"`},
		{name: "batch-flow-list", script: "flow list\n", want: 0, contains: "Patient Admit"},
		{name: "batch-unknown-command", script: "nonsense\n", want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "script.txt")
			if err := os.WriteFile(path, []byte(tt.script), 0o600); err != nil {
				t.Fatal(err)
			}
			var out, errb bytes.Buffer
			if code := run([]string{"-a", ts.URL, "-s", path}, strings.NewReader(""), &out, &errb); code != tt.want {
				t.Fatalf("exit = %d, want %d (stderr %q)", code, tt.want, errb.String())
			}
			if !strings.Contains(out.String(), tt.contains) {
				t.Errorf("stdout %q does not contain %q", out.String(), tt.contains)
			}
		})
	}

	t.Run("server-subcommand", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := ln.Addr().String()
		_ = ln.Close()
		runUntilSIGTERM(t, []string{"server", addr}, "http://"+addr+"/api/openapi.yaml")
	})

	t.Run("no-subcommand", func(t *testing.T) {
		const addr = "127.0.0.1:8080"
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			// The default address is taken on this host: a bare `weavster`
			// must still try to start the server there and report the bind error.
			var out, errb bytes.Buffer
			if code := run(nil, strings.NewReader(""), &out, &errb); code != 1 || !strings.Contains(errb.String(), addr) {
				t.Fatalf("exit = %d, stderr %q; want bind error on %s", code, errb.String(), addr)
			}
			return
		}
		_ = ln.Close()
		runUntilSIGTERM(t, nil, "http://"+addr+"/api/openapi.yaml")
	})
}

// TestSupportMatrixPrivilegedGuard proves `weavster server` refuses a
// privileged account and that WEAVSTER_ALLOW_ROOT=1 overrides the refusal.
func TestSupportMatrixPrivilegedGuard(t *testing.T) {
	orig := isPrivileged
	isPrivileged = func() bool { return true }
	t.Cleanup(func() { isPrivileged = orig })

	// An occupied address makes an allowed server exit instead of blocking.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	tests := []struct {
		name      string
		allowRoot string
		want      string
	}{
		{name: "refused", allowRoot: "", want: "refusing to run under a privileged OS account"},
		{name: "override", allowRoot: "1", want: "address already in use"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("WEAVSTER_ALLOW_ROOT", tt.allowRoot)
			var out, errb bytes.Buffer
			if code := run([]string{"server", ln.Addr().String()}, strings.NewReader(""), &out, &errb); code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			if !strings.Contains(errb.String(), tt.want) {
				t.Errorf("stderr %q does not contain %q", errb.String(), tt.want)
			}
		})
	}
}

// runUntilSIGTERM runs the CLI with args, waits until readyURL answers, then
// sends SIGTERM and expects a clean exit 0.
func runUntilSIGTERM(t *testing.T, args []string, readyURL string) {
	t.Helper()
	done := make(chan int, 1)
	var out, errb bytes.Buffer
	go func() { done <- run(args, strings.NewReader(""), &out, &errb) }()
	waitReady(t, readyURL)
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit = %d, want 0 after SIGTERM (stderr %q)", code, errb.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down after SIGTERM")
	}
}

func waitReady(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if resp, err := http.Get(url); err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server at %s never became ready", url)
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
		if len(cells) != 7 || !strings.HasPrefix(strings.TrimSpace(cells[1]), "`") {
			continue
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		documented = append(documented, strings.Join([]string{strings.Trim(cells[1], "`"), cells[2], cells[3], cells[4], cells[5]}, " | "))
	}

	var want []string
	for _, e := range codecs.CoverageMatrix() {
		tier, ack := "Library-only", "no"
		if e.Enterprise {
			tier = "Enterprise-deferred"
		}
		if e.Acknowledgment {
			ack = "yes"
		}
		want = append(want, strings.Join([]string{e.Name, tier, e.Versions, ack, e.Notes}, " | "))
	}
	sort.Strings(documented)
	sort.Strings(want)
	if got, exp := strings.Join(documented, "\n"), strings.Join(want, "\n"); got != exp {
		t.Errorf("docs codec table:\n%s\nCoverageMatrix:\n%s", got, exp)
	}
}
