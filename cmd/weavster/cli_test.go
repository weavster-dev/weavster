package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

// TestHTTPClientGetSetsMarkerHeader ensures every outbound request carries
// the CSRF marker header required by the gateway (spec §2.13.45, §10).
func TestHTTPClientGetSetsMarkerHeader(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get(gateway.MarkerHeader)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newHTTPClient(srv.URL, "", "")
	resp, err := c.get(context.Background(), "/anything")
	if err != nil {
		t.Fatalf("get() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if gotHeader != gateway.MarkerValue {
		t.Errorf("marker header = %q, want %q", gotHeader, gateway.MarkerValue)
	}
}

func TestHTTPClientGetInvalidURL(t *testing.T) {
	c := newHTTPClient("http://[::1]:namedport", "", "")
	if _, err := c.get(context.Background(), "/x"); err == nil {
		t.Error("expected error for invalid request URL, got nil")
	}
}

func TestHTTPClientStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system" {
			t.Errorf("path = %q, want /api/v1/system", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"name":"weavster"}`))
	}))
	defer srv.Close()

	c := newHTTPClient(srv.URL, "", "")
	out, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if out != `{"name":"weavster"}` {
		t.Errorf("Status() = %q", out)
	}
}

func TestHTTPClientStatusError(t *testing.T) {
	c := newHTTPClient("http://[::1]:namedport", "", "")
	if _, err := c.Status(context.Background()); err == nil {
		t.Error("expected error, got nil")
	}
}

func TestHTTPClientCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	if _, err := c.Call(context.Background(), http.MethodGet, "/missing", nil); err == nil || err.Error() != "server returned 404 Not Found: flow not found" {
		t.Errorf("Call error = %v", err)
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

func (replyClient) Status(context.Context) (string, error)     { return "", nil }
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
