package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/weavster-dev/weavster/internal/gateway"
)

// Client is the network-API surface used by the scriptable shell (spec §3).
type Client interface {
	Status(ctx context.Context) (string, error)
	FlowList(ctx context.Context) ([]string, error)
	UserList(ctx context.Context) ([]string, error)
	Version(ctx context.Context) string
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

func (c *httpClient) get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(gateway.MarkerHeader, gateway.MarkerValue)
	return c.http.Do(req)
}

func (c *httpClient) Status(ctx context.Context) (string, error) {
	resp, err := c.get(ctx, "/api/v1/system")
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return string(body), nil
}

func (c *httpClient) FlowList(ctx context.Context) ([]string, error) {
	resp, err := c.get(ctx, "/api/v1/flows")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var flows []gateway.Flow
	if err := json.NewDecoder(resp.Body).Decode(&flows); err != nil {
		return nil, err
	}
	names := make([]string, len(flows))
	for i, f := range flows {
		names[i] = f.Name
	}
	return names, nil
}

func (c *httpClient) UserList(ctx context.Context) ([]string, error) {
	// MVP: user listing is not exposed over REST yet; return empty.
	return nil, nil
}

func (c *httpClient) Version(context.Context) string { return version }

// runScript executes shell commands line-by-line (batch -s mode), returning
// the §3.3 exit code.
func runScript(script []byte, client Client, stdout, stderr io.Writer, debug bool) int {
	sc := bufio.NewScanner(bytes.NewReader(script))
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
	return rc
}
