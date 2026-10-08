package adapters

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"time"
)

// HTTPSink sends messages to a URL (POST unless configured otherwise).
type HTTPSink struct {
	url    string
	method string
	client *http.Client
}

// HTTPSinkTimeout bounds one delivery request, including reading the
// response, unless HTTPSinkOptions.Timeout sets another bound.
const HTTPSinkTimeout = 30 * time.Second

// HTTPSinkOptions shape an HTTP sink's requests; zero values are the
// defaults: POST, HTTPSinkTimeout, and no redirects followed.
type HTTPSinkOptions struct {
	Method  string
	Timeout time.Duration
	// MaxRedirects is how many 307/308 redirects are followed (they keep
	// the method and body). Other redirects, and https to http, are never
	// followed: the 3xx reply fails the delivery.
	MaxRedirects int
}

// NewHTTPSink returns an HTTP sink posting to url with the default options.
func NewHTTPSink(url string) *HTTPSink {
	return NewHTTPSinkWith(url, HTTPSinkOptions{})
}

// NewHTTPSinkWith returns an HTTP sink sending to url with opts.
func NewHTTPSinkWith(url string, opts HTTPSinkOptions) *HTTPSink {
	method, timeout := opts.Method, opts.Timeout
	if method == "" {
		method = http.MethodPost
	}
	if timeout <= 0 { // never unbounded
		timeout = HTTPSinkTimeout
	}
	return &HTTPSink{url: url, method: method, client: &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			code := req.Response.StatusCode
			keeps := code == http.StatusTemporaryRedirect || code == http.StatusPermanentRedirect
			downgrade := via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" // on any hop
			if !keeps || downgrade || len(via) > opts.MaxRedirects {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}}
}

func (s *HTTPSink) Name() string { return "http" }

func (s *HTTPSink) Write(ctx context.Context, m Message) error {
	_, err := s.send(ctx, m, false)
	return err
}

// MaxResponseBytes is the largest reply WriteResponse returns.
const MaxResponseBytes = 1 << 20

// Reply is the response to a successful HTTP delivery.
type Reply struct {
	Body        []byte
	ContentType string
}

// WriteResponse is Write that also returns the reply of a successful
// delivery. The reply is nil when it could not be read completely or is
// larger than MaxResponseBytes; the delivery still succeeded.
func (s *HTTPSink) WriteResponse(ctx context.Context, m Message) (*Reply, error) {
	return s.send(ctx, m, true)
}

func (s *HTTPSink) send(ctx context.Context, m Message, wantReply bool) (*Reply, error) {
	req, err := http.NewRequestWithContext(ctx, s.method, s.url, bytes.NewReader(m.Body))
	if err != nil {
		return nil, err
	}
	contentType := m.Metadata[ContentTypeMetadata]
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	req.Header.Set("Content-Type", contentType)
	if key := m.Metadata[IdempotencyKeyMetadata]; key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 || !wantReply {
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode >= 300 {
			return nil, &httpStatusError{code: resp.StatusCode, location: resp.Header.Get("Location")}
		}
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil || len(body) > MaxResponseBytes {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, nil // delivered; the reply is unusable
	}
	return &Reply{Body: body, ContentType: resp.Header.Get("Content-Type")}, nil
}

type httpStatusError struct {
	code     int
	location string // a redirect's target, not followed
}

// Code is "http:<status>".
func (e *httpStatusError) Code() string { return "http:" + strconv.Itoa(e.code) }

func (e *httpStatusError) Error() string {
	if e.code < 400 && e.location != "" {
		return http.StatusText(e.code) + ": redirect to " + e.location + " not followed"
	}
	return http.StatusText(e.code)
}

func (s *HTTPSink) Close() error { return nil }

// HTTPSource accepts POST requests on path and buffers them for Read.
type HTTPSource struct {
	path string
	ch   chan Message
}

// NewHTTPSource returns an HTTP listener source for the given path.
func NewHTTPSource(path string) *HTTPSource {
	return &HTTPSource{path: path, ch: make(chan Message, 64)}
}

func (s *HTTPSource) Name() string { return "http" }

// Handler returns the HTTP handler to mount on a server.
func (s *HTTPSource) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != s.path || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		select {
		case s.ch <- Message{ID: r.Header.Get("X-Message-Id"), Body: body}:
			w.WriteHeader(http.StatusAccepted)
		case <-r.Context().Done():
			http.Error(w, "source closed", http.StatusServiceUnavailable)
		}
	})
}

func (s *HTTPSource) Read(ctx context.Context) (Message, error) {
	select {
	case m, ok := <-s.ch:
		if !ok {
			return Message{}, io.EOF
		}
		return m, nil
	case <-ctx.Done():
		return Message{}, ctx.Err()
	}
}

func (s *HTTPSource) Close() error {
	close(s.ch)
	return nil
}
