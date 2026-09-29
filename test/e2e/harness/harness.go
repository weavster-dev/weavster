// Package harness runs the weavster binary as a separate process for the
// black-box end-to-end suites: it builds the binary, starts the server with
// a configuration and environment, waits until it answers, and stops it
// with a signal (SIGTERM for a clean stop, SIGKILL for a crash).
package harness

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// AdminPassword is the bootstrap admin password Start gives every server.
const AdminPassword = "E2e-Admin-Pass-1"

// Build compiles the weavster binary into dir and returns its path.
func Build(dir string) (string, error) {
	bin := filepath.Join(dir, "weavster")
	out, err := exec.Command("go", "build", "-o", bin, "github.com/weavster-dev/weavster/cmd/weavster").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("go build: %w: %s", err, out)
	}
	return bin, nil
}

// FreeAddr returns a loopback address nothing listens on.
func FreeAddr() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().String(), nil
}

// Server is a running weavster server process.
type Server struct {
	cmd  *exec.Cmd
	log  *syncBuffer
	done chan struct{}
	err  error // the process's exit, once done is closed
}

// Start runs bin as a server with config (YAML, written into dir) and env
// added to this process's environment, and waits until readyURL answers
// (a TLS client is given client, else http.DefaultClient is used). It
// fails if the server exits first or does not answer within 30 seconds.
func Start(bin, dir, config, readyURL string, client *http.Client, env ...string) (*Server, error) {
	path := filepath.Join(dir, "weavster.yaml")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		return nil, err
	}
	s := &Server{log: &syncBuffer{}, done: make(chan struct{})}
	s.cmd = exec.Command(bin, "server", "--config", path)
	s.cmd.Env = append(append(os.Environ(), "WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD="+AdminPassword), env...)
	s.cmd.Stdout, s.cmd.Stderr = s.log, s.log
	if err := s.cmd.Start(); err != nil {
		return nil, err
	}
	go func() { s.err = s.cmd.Wait(); close(s.done) }()
	if client == nil {
		client = http.DefaultClient
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		select {
		case <-s.done:
			return nil, fmt.Errorf("the server exited before it answered (%v):\n%s", s.err, s.Log())
		default:
		}
		if resp, err := client.Get(readyURL); err == nil {
			_ = resp.Body.Close()
			return s, nil
		}
		if time.Now().After(deadline) {
			s.Kill()
			return nil, fmt.Errorf("the server did not answer %s within 30 seconds:\n%s", readyURL, s.Log())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Stop sends SIGTERM and returns the exit code once the server has
// stopped (-1 if it did not stop within 30 seconds; it is then killed).
func (s *Server) Stop() int {
	_ = s.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-s.done:
	case <-time.After(30 * time.Second):
		s.Kill()
		return -1
	}
	var exit *exec.ExitError
	if errors.As(s.err, &exit) {
		return exit.ExitCode()
	}
	return 0
}

// Kill ends the server with SIGKILL, as a crash would, and waits for it.
func (s *Server) Kill() {
	_ = s.cmd.Process.Kill()
	<-s.done
}

// Log is everything the server has written to stdout and stderr.
func (s *Server) Log() string { return s.log.String() }

// TLSClient trusts only the CA in caPEM and speaks at most maxVersion
// (0: any version).
func TLSClient(caPEM []byte, maxVersion uint16) (*http.Client, error) {
	pool, err := certPool(caPEM)
	if err != nil {
		return nil, err
	}
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, MaxVersion: maxVersion},
	}}, nil
}

// Request sends one API request: body as JSON (none if empty), the CSRF
// marker unless noCSRF, and the given user's basic credentials (none if
// user is empty). It returns the status and body.
func Request(client *http.Client, method, url, body, user, password string, noCSRF bool) (int, string, http.Header, error) {
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		return 0, "", nil, err
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if !noCSRF {
		req.Header.Set("X-Weavster-CSRF", "1")
	}
	if user != "" {
		req.SetBasicAuth(user, password)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var b bytes.Buffer
	_, err = b.ReadFrom(resp.Body)
	return resp.StatusCode, b.String(), resp.Header, err
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}
