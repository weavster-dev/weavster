package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestHTTPClientRefusesRedirects(t *testing.T) {
	for _, code := range []int{301, 302, 303, 307, 308} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			for _, sameOrigin := range []bool{false, true} {
				name := "https-to-http"
				if sameOrigin {
					name = "same-origin"
				}
				t.Run(name, func(t *testing.T) {
					var requests, credentials, bodies atomic.Int32
					target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						requests.Add(1)
						if _, _, ok := r.BasicAuth(); ok {
							credentials.Add(1)
						}
						body, _ := io.ReadAll(r.Body)
						if len(body) > 0 {
							bodies.Add(1)
						}
						w.WriteHeader(http.StatusOK)
					})
					plain := httptest.NewServer(target)
					defer plain.Close()
					origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.URL.Path == "/target" {
							target.ServeHTTP(w, r)
							return
						}
						location := plain.URL + "/target"
						if sameOrigin {
							location = "/target"
						}
						w.Header().Set("Location", location)
						w.WriteHeader(code)
					}))
					defer origin.Close()

					c := newHTTPClient(origin.URL, "test-user", "test-password")
					// Preserve the constructor's redirect policy, trusting only
					// the local test server without mutating http.DefaultClient.
					client := *c.http
					client.Transport = origin.Client().Transport
					c.http = &client
					_, err := c.Call(context.Background(), http.MethodPost, "/redirect", []byte("synthetic message"))
					var statusErr *serverError
					if !errors.As(err, &statusErr) || statusErr.Code != code {
						t.Errorf("Call must return the original HTTP %d error", code)
					}
					if requests.Load() != 0 {
						t.Errorf("redirect followed: requests=%d authenticated=%d with-body=%d", requests.Load(), credentials.Load(), bodies.Load())
					}
				})
			}
		})
	}
}

// TestHTTPClientWithCARefusesRedirects: -ca replaces the client's transport
// and keeps refusing redirects.
func TestHTTPClientWithCARefusesRedirects(t *testing.T) {
	c := newHTTPClient("https://127.0.0.1:1", "u", "p")
	if err := c.withCA(""); err != nil {
		t.Fatal(err)
	}
	if c.http.CheckRedirect == nil || c.http.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
		t.Error("the client from -ca follows redirects")
	}
}
