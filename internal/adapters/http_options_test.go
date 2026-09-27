package adapters

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestHTTPSinkOptions: the method and timeout apply; only 307/308
// redirects are followed, up to MaxRedirects, with the method and body.
func TestHTTPSinkOptions(t *testing.T) {
	var mu sync.Mutex
	var got []string // "METHOD path body"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, r.Method+" "+r.URL.Path+" "+string(b))
		mu.Unlock()
		switch r.URL.Path {
		case "/found":
			http.Redirect(w, r, "/final", http.StatusFound)
		case "/temporary":
			http.Redirect(w, r, "/final", http.StatusTemporaryRedirect)
		case "/twice":
			http.Redirect(w, r, "/temporary", http.StatusPermanentRedirect)
		case "/slow":
			time.Sleep(300 * time.Millisecond)
		}
	}))
	defer srv.Close()
	for _, tt := range []struct {
		name, path string
		opts       HTTPSinkOptions
		ok         bool
		want       string
	}{
		{"default POST", "/final", HTTPSinkOptions{}, true, "POST /final x"},
		{"PUT", "/final", HTTPSinkOptions{Method: http.MethodPut}, true, "PUT /final x"},
		{"307 not followed by default", "/temporary", HTTPSinkOptions{}, false, "POST /temporary x"},
		{"307 followed", "/temporary", HTTPSinkOptions{MaxRedirects: 1}, true, "POST /temporary x|POST /final x"},
		{"302 never followed", "/found", HTTPSinkOptions{MaxRedirects: 5}, false, "POST /found x"},
		{"too many", "/twice", HTTPSinkOptions{Method: http.MethodPatch, MaxRedirects: 1}, false, "PATCH /twice x|PATCH /temporary x"},
		{"enough", "/twice", HTTPSinkOptions{Method: http.MethodPatch, MaxRedirects: 2}, true, "PATCH /twice x|PATCH /temporary x|PATCH /final x"},
		{"timeout", "/slow", HTTPSinkOptions{Timeout: 50 * time.Millisecond}, false, "POST /slow x"},
		{"negative timeout is the default", "/final", HTTPSinkOptions{Timeout: -time.Second}, true, "POST /final x"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mu.Lock()
			got = nil
			mu.Unlock()
			err := NewHTTPSinkWith(srv.URL+tt.path, tt.opts).Write(context.Background(), Message{Body: []byte("x")})
			if (err == nil) != tt.ok {
				t.Errorf("err = %v, want ok=%v", err, tt.ok)
			}
			mu.Lock()
			defer mu.Unlock()
			if strings.Join(got, "|") != tt.want {
				t.Errorf("requests = %q, want %q", strings.Join(got, "|"), tt.want)
			}
		})
	}
}

// TestHTTPSinkNeverDowngrades: a redirect from https to http is not
// followed, whatever MaxRedirects allows.
func TestHTTPSinkNeverDowngrades(t *testing.T) {
	check := NewHTTPSinkWith("https://example.com/in", HTTPSinkOptions{MaxRedirects: 5}).client.CheckRedirect
	from, _ := url.Parse("https://example.com/in")
	for _, tt := range []struct {
		to   string
		want error
	}{
		{"https://example.com/next", nil},
		{"http://example.com/next", http.ErrUseLastResponse},
	} {
		to, _ := url.Parse(tt.to)
		req := &http.Request{URL: to, Response: &http.Response{StatusCode: http.StatusTemporaryRedirect}}
		if err := check(req, []*http.Request{{URL: from}}); err != tt.want {
			t.Errorf("%s: %v, want %v", tt.to, err, tt.want)
		}
	}
	// A later hop: http -> https -> http is refused at the second hop.
	start, _ := url.Parse("http://a.example.com/in")
	down, _ := url.Parse("http://c.example.com/in")
	req := &http.Request{URL: down, Response: &http.Response{StatusCode: http.StatusTemporaryRedirect}}
	if err := check(req, []*http.Request{{URL: start}, {URL: from}}); err != http.ErrUseLastResponse {
		t.Errorf("https to http on a later hop: %v", err)
	}
}

// TestHTTPSinkRedirectError: a redirect that is not followed names its target.
func TestHTTPSinkRedirectError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://new.example.com/in", http.StatusFound)
	}))
	defer srv.Close()
	err := NewHTTPSink(srv.URL).Write(context.Background(), Message{Body: []byte("x")})
	if err == nil || err.Error() != "Found: redirect to https://new.example.com/in not followed" {
		t.Errorf("err = %v", err)
	}
}
