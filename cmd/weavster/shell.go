package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/weavster-dev/weavster/internal/flowlife"
	"github.com/weavster-dev/weavster/internal/gateway"
)

// shellPrompt is printed before each interactive command.
const shellPrompt = "weavster> "

// maxShellLine is the longest command line the shell reads.
const maxShellLine = 1 << 20

// runShell is the interactive remote shell (spec §3): it reads commands
// from in until quit, exit, or end of input. Errors are printed and the
// shell continues; it exits 0.
func runShell(in io.Reader, client Client, stdout, stderr io.Writer, debug bool) int {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64<<10), maxShellLine+2) // room for "\r\n"
	for {
		_, _ = fmt.Fprint(stdout, shellPrompt)
		if !sc.Scan() {
			_, _ = fmt.Fprintln(stdout)
			if err := sc.Err(); err != nil {
				return shellError(stderr, debug, fmt.Errorf("reading commands: %w", err))
			}
			return 0
		}
		if len(sc.Bytes()) > maxShellLine {
			_, _ = fmt.Fprintln(stdout)
			return shellError(stderr, debug, fmt.Errorf("reading commands: %w", bufio.ErrTooLong))
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if first := strings.Fields(line)[0]; first == "quit" || first == "exit" {
			return 0
		}
		_ = dispatch(context.Background(), client, line, stdout, stderr, debug)
	}
}

// connection is a connection file (-c): where and as whom to connect.
type connection struct {
	Address  string `yaml:"address"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

// loadConnection reads a connection file; unknown keys are rejected.
func loadConnection(path string) (connection, error) {
	var c connection
	data, err := os.ReadFile(path)
	if err != nil {
		return c, fmt.Errorf("connection file: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil && err != io.EOF {
		return c, fmt.Errorf("connection file %s: %w", path, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return c, fmt.Errorf("connection file %s: must hold a single YAML document", path)
	}
	return c, nil
}

// override returns the file's settings with those set in flags (non-empty)
// taking precedence.
func (c connection) override(flags connection) connection {
	if flags.Address != "" {
		c.Address = flags.Address
	}
	if flags.User != "" {
		c.User = flags.User
	}
	if flags.Password != "" {
		c.Password = flags.Password
	}
	return c
}

// serverVersion asks the server for its version (-v).
func serverVersion(ctx context.Context, client Client) (string, error) {
	out, err := client.Call(ctx, http.MethodGet, "/api/v1/system", nil)
	if err != nil {
		return "", err
	}
	var info struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(out, &info); err != nil || info.Version == "" {
		return "", fmt.Errorf("server returned no version")
	}
	return info.Version, nil
}

// login checks the client's credentials against the server with a call
// that also refuses an account that must change its password first.
func (c *httpClient) login(ctx context.Context) error {
	_, err := c.Call(ctx, http.MethodGet, "/api/v1/system", nil)
	return err
}

// deployedStatus prints the status of every deployed flow (spec §3.2
// status), or "no deployed flows".
func deployedStatus(ctx context.Context, client Client, stdout, stderr io.Writer, debug bool) int {
	n, err := printFlows(ctx, client, stdout, func(f gateway.Flow) bool { return f.Status != flowlife.Undeployed })
	if err != nil {
		return shellError(stderr, debug, err)
	}
	if n == 0 {
		_, _ = fmt.Fprintln(stdout, "no deployed flows")
	}
	return 0
}

// printFlows prints the flows that keep selects, one line each: id, status,
// and name, tab-separated. It returns how many it printed.
func printFlows(ctx context.Context, client Client, stdout io.Writer, keep func(gateway.Flow) bool) (int, error) {
	flows, err := listFlows(ctx, client)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, f := range flows {
		if keep(f) {
			_, _ = fmt.Fprintln(stdout, f.ID+"\t"+f.Status+"\t"+escapeControl(f.Name))
			n++
		}
	}
	return n, nil
}
