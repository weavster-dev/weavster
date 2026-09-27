package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/gateway"
)

// TestNewHTTPClientDefaultAddr verifies the client falls back to the local
// default address when none is supplied (spec §3.2).
func TestNewHTTPClientDefaultAddr(t *testing.T) {
	c := newHTTPClient("", "user", "pass")
	if c.base != "http://127.0.0.1:8080" {
		t.Errorf("base = %q, want default addr", c.base)
	}
	if c.user != "user" || c.pass != "pass" {
		t.Errorf("user/pass not set correctly: %+v", c)
	}
}

func TestNewHTTPClientExplicitAddr(t *testing.T) {
	c := newHTTPClient("http://example.com:9090", "", "")
	if c.base != "http://example.com:9090" {
		t.Errorf("base = %q, want explicit addr", c.base)
	}
}

// TestHTTPClientSetsMarkerHeader ensures every outbound request carries
// the CSRF marker header required by the gateway (spec §2.13.45, §10).
func TestHTTPClientSetsMarkerHeader(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get(gateway.MarkerHeader)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newHTTPClient(srv.URL, "", "")
	if _, err := c.Call(context.Background(), http.MethodGet, "/anything", nil); err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if gotHeader != gateway.MarkerValue {
		t.Errorf("marker header = %q, want %q", gotHeader, gateway.MarkerValue)
	}
}

func TestHTTPClientInvalidURL(t *testing.T) {
	c := newHTTPClient("http://[::1]:namedport", "", "")
	if _, err := c.Call(context.Background(), http.MethodGet, "/x", nil); err == nil {
		t.Error("expected error for invalid request URL, got nil")
	}
}

func TestHTTPClientCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/partial" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"code":"IMPORT_INCOMPLETE","message":"import stopped part-way"},"created":["a"],"updated":[]}`))
			return
		}
		if r.URL.Path == "/envelope" {
			w.WriteHeader(http.StatusNotImplemented)
			_, _ = w.Write([]byte(`{"error":{"code":"NOT_IMPLEMENTED","message":"not implemented in this edition: SSO"}}`))
			return
		}
		if r.URL.Path == "/missing" {
			http.Error(w, "flow not found", http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		_, _ = w.Write([]byte(r.Method + " " + string(body)))
	}))
	defer srv.Close()

	c := newHTTPClient(srv.URL, "", "")
	out, err := c.Call(context.Background(), http.MethodPut, "/x", []byte("doc"))
	if err != nil || string(out) != "PUT doc" {
		t.Errorf("Call = %q, %v", out, err)
	}
	if _, err := c.Call(context.Background(), http.MethodGet, "/missing", nil); err == nil || err.Error() != "server returned 404 Not Found: flow not found" { // not an envelope: raw body
		t.Errorf("Call error = %v", err)
	}
	if _, err := c.Call(context.Background(), http.MethodGet, "/partial", nil); err == nil ||
		err.Error() != `server returned 500 Internal Server Error: import stopped part-way {"created":["a"],"updated":[]}` {
		t.Errorf("partial error = %v", err)
	}
	if _, err := c.Call(context.Background(), http.MethodGet, "/envelope", nil); err == nil || err.Error() != "server returned 501 Not Implemented: not implemented in this edition: SSO" {
		t.Errorf("envelope error = %v", err)
	}
	if _, err := newHTTPClient("http://[::1]:namedport", "", "").Call(context.Background(), http.MethodGet, "/x", nil); err == nil {
		t.Error("expected request error, got nil")
	}
}

func TestFlowListUnreadableReply(t *testing.T) {
	var out, errb bytes.Buffer
	if code := flowCommand(context.Background(), replyClient("not json"), []string{"list"}, &out, &errb, false); code != 2 {
		t.Errorf("exit = %d, want 2 (stderr %q)", code, errb.String())
	}
	errb.Reset()
	if code := flowCommand(context.Background(), replyClient("[1]"), []string{"rename", "a", "B"}, &out, &errb, false); code != 2 || !strings.Contains(errb.String(), "unreadable flow") {
		t.Errorf("rename of an unreadable flow: exit %d, stderr %q", code, errb.String())
	}
}

// replyClient answers every Call with the same body.
type replyClient string

func (replyClient) UserList(context.Context) ([]string, error) { return nil, nil }
func (replyClient) Version(context.Context) string             { return version }
func (r replyClient) Call(context.Context, string, string, []byte) ([]byte, error) {
	return []byte(r), nil
}

// TestHTTPClientUserList documents the MVP behaviour: user listing is not
// yet exposed over REST, so the client always returns an empty result.
func TestHTTPClientUserList(t *testing.T) {
	c := newHTTPClient("http://example.invalid", "", "")
	names, err := c.UserList(context.Background())
	if err != nil {
		t.Fatalf("UserList() error = %v", err)
	}
	if names != nil {
		t.Errorf("UserList() = %v, want nil", names)
	}
}

func TestHTTPClientVersion(t *testing.T) {
	c := newHTTPClient("http://example.invalid", "", "")
	if got := c.Version(context.Background()); got != version {
		t.Errorf("Version() = %q, want %q", got, version)
	}
}

func TestEscapeControl(t *testing.T) {
	for in, want := range map[string]string{"ADT": "ADT", "a\tb": `a\tb`, "a\nb": `a\nb`, "é\x01": `é\x01`} {
		if got := escapeControl(in); got != want {
			t.Errorf("escapeControl(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitArgs(t *testing.T) {
	tests := []struct {
		line    string
		want    []string
		wantErr bool
	}{
		{"flow list", []string{"flow", "list"}, false},
		{"  flow \t get   adt ", []string{"flow", "get", "adt"}, false},
		{`flow rename adt "ADT Inbound"`, []string{"flow", "rename", "adt", "ADT Inbound"}, false},
		{`export * "My Flows/all.json"`, []string{"export", "*", "My Flows/all.json"}, false},
		{`flow get ""`, []string{"flow", "get", ""}, false},
		{`a "say \"hi\" \\ ok"`, []string{"a", `say "hi" \ ok`}, false},
		{`a pre"fix"`, []string{"a", "prefix"}, false},
		{`a \n`, []string{"a", `\n`}, false},
		{"flow get\u00a0adt\v x", []string{"flow", "get", "adt", "x"}, false},
		{`a "é ü"`, []string{"a", "é ü"}, false},
		{`a "open`, nil, true},
	}
	for _, tt := range tests {
		got, err := splitArgs(tt.line)
		if (err != nil) != tt.wantErr || strings.Join(got, "|") != strings.Join(tt.want, "|") || len(got) != len(tt.want) {
			t.Errorf("splitArgs(%q) = %q, %v; want %q", tt.line, got, err, tt.want)
		}
	}
}

// slowDeployClient lists two enabled, undeployed flows and takes delay to
// answer each deploy.
type slowDeployClient struct{ delay time.Duration }

func (slowDeployClient) UserList(context.Context) ([]string, error) { return nil, nil }
func (slowDeployClient) Version(context.Context) string             { return version }
func (c slowDeployClient) Call(_ context.Context, method, path string, _ []byte) ([]byte, error) {
	switch {
	case path == "/api/v1/flows":
		return []byte(`[{"id":"a","status":"undeployed","enabled":true},{"id":"b","status":"undeployed","enabled":true}]`), nil
	case method == http.MethodPost:
		time.Sleep(c.delay)
		return []byte(`{}`), nil
	}
	return []byte(`{"status":"undeployed"}`), nil
}

func TestFlowUsageCheckedFirst(t *testing.T) {
	var out, errb bytes.Buffer
	// The erroring client fails every request: a usage error must not
	// reach it.
	if code := flowCommand(context.Background(), erroringClient{}, []string{"get", "export", "extra"}, &out, &errb, false); code != 2 || !strings.Contains(errb.String(), "usage:") || strings.Contains(errb.String(), "unavailable") {
		t.Errorf("exit %d, stderr %q", code, errb.String())
	}
}

func TestDeployTimeoutStopsNewDeploys(t *testing.T) {
	var out, errb bytes.Buffer
	code := deployAll(context.Background(), slowDeployClient{delay: 1100 * time.Millisecond}, []string{"1"}, &out, &errb, false)
	if code != 2 || !strings.Contains(out.String(), "deployed a\ndeployed 1 flows") || !strings.Contains(errb.String(), "timeout: deploy stopped before b") {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out.String(), errb.String())
	}
}
