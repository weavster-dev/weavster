package adapters

import (
	"bytes"
	"context"
	"io"
	"net/http"
)

// HTTPSink POSTs messages to a URL.
type HTTPSink struct {
	url    string
	client *http.Client
}

// NewHTTPSink returns an HTTP sink posting to url.
func NewHTTPSink(url string) *HTTPSink {
	return &HTTPSink{url: url, client: http.DefaultClient}
}

func (s *HTTPSink) Name() string { return "http" }

func (s *HTTPSink) Write(ctx context.Context, m Message) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(m.Body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return &httpStatusError{code: resp.StatusCode}
	}
	return nil
}

type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string { return http.StatusText(e.code) }

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
