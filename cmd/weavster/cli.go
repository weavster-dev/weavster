package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/weavster-dev/weavster/internal/gateway"
)

// Client is the network-API surface used by the scriptable shell (spec §3).
type Client interface {
	Version(ctx context.Context) string
	// Call sends one REST request and returns the response body. A reply
	// that is not 2xx is an error carrying the status and the body.
	Call(ctx context.Context, method, path string, body []byte) ([]byte, error)
}

// httpClient is the REST Client adapter (spec §3.2, §3.3).
type httpClient struct {
	base string
	user string
	pass string
	http *http.Client
}

func newHTTPClient(addr, user, pass string) *httpClient {
	if addr == "" {
		addr = "http://127.0.0.1:8080"
	}
	return &httpClient{base: addr, user: user, pass: pass, http: http.DefaultClient}
}

func (c *httpClient) request(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set(gateway.MarkerHeader, gateway.MarkerValue)
	if c.user != "" {
		req.SetBasicAuth(c.user, c.pass)
	}
	return c.http.Do(req)
}

func (c *httpClient) Call(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	resp, err := c.request(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, &serverError{Code: resp.StatusCode, Status: resp.Status, Body: strings.TrimSpace(string(out))}
	}
	return out, nil
}

// serverError is a reply that is not 2xx.
type serverError struct {
	Code   int
	Status string // e.g. "404 Not Found"
	Body   string
}

// Error shows the message of the server's JSON error envelope followed by
// any other fields of the reply (such as what a stopped import already
// wrote), or the raw body when the reply is not an envelope.
func (e *serverError) Error() string {
	var fields map[string]json.RawMessage
	var env struct {
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(e.Body), &fields) != nil || json.Unmarshal(fields["error"], &env) != nil || env.Message == "" {
		return "server returned " + e.Status + ": " + e.Body
	}
	msg := env.Message
	delete(fields, "error")
	if len(fields) > 0 {
		extra, _ := json.Marshal(fields) // raw JSON values re-encode
		msg += " " + string(extra)
	}
	return "server returned " + e.Status + ": " + msg
}

func (c *httpClient) Version(context.Context) string { return version }

// runScript executes shell commands line-by-line (batch -s mode), returning
// the §3.3 exit code.
func runScript(script []byte, client Client, stdout, stderr io.Writer, debug bool) int {
	sc := bufio.NewScanner(bytes.NewReader(script))
	sc.Buffer(make([]byte, 0, 64<<10), maxShellLine+2) // the shell's line limit
	rc := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if code := dispatch(context.Background(), client, line, stdout, stderr, debug); code == 2 {
			rc = 2
		}
	}
	if err := sc.Err(); err != nil { // a line over the limit ends the script
		return shellError(stderr, debug, fmt.Errorf("reading the script: %w", err))
	}
	return rc
}
