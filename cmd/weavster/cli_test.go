package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	defer resp.Body.Close()
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

func TestHTTPClientFlowList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/flows" {
			t.Errorf("path = %q, want /api/v1/flows", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]gateway.Flow{{Name: "flow-a"}, {Name: "flow-b"}})
	}))
	defer srv.Close()

	c := newHTTPClient(srv.URL, "", "")
	names, err := c.FlowList(context.Background())
	if err != nil {
		t.Fatalf("FlowList() error = %v", err)
	}
	if len(names) != 2 || names[0] != "flow-a" || names[1] != "flow-b" {
		t.Errorf("FlowList() = %v", names)
	}
}

func TestHTTPClientFlowListRequestError(t *testing.T) {
	c := newHTTPClient("http://[::1]:namedport", "", "")
	if _, err := c.FlowList(context.Background()); err == nil {
		t.Error("expected error, got nil")
	}
}

func TestHTTPClientFlowListDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c := newHTTPClient(srv.URL, "", "")
	if _, err := c.FlowList(context.Background()); err == nil {
		t.Error("expected decode error, got nil")
	}
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
