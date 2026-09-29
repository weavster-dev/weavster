// Package harness runs the weavster binary as a separate process for the
// black-box end-to-end suites: it builds the binary, starts the server with
// a configuration and environment, waits until it answers, and stops it
// with a signal (SIGTERM for a clean stop, SIGKILL for a crash).
package harness

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
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
	// Password is the admin account's password.
	Password string
	cmd      *exec.Cmd
	log      *syncBuffer
	done     chan struct{}
	err      error // the process's exit, once done is closed
}

// Options describe a server to start.
type Options struct {
	Config  string       // the server configuration (YAML)
	BaseURL string       // where it answers, for example http://127.0.0.1:8080
	Client  *http.Client // for an https BaseURL; nil: a plain client
	// Password is the bootstrap admin password; empty: a new random one.
	// A restart on the same store passes the first start's.
	Password string
	Env      []string // added to this process's environment
}

// Start runs bin as a server with o.Config (written into dir) and waits
// until it answers at o.BaseURL. The admin password is unique to this
// server, and the server counts as started only when it accepts it, so
// another server that happens to answer on the address is never taken for
// this one. Start fails if the server exits first or does not answer
// within 30 seconds.
func Start(bin, dir string, o Options) (*Server, error) {
	path := filepath.Join(dir, "weavster.yaml")
	if err := os.WriteFile(path, []byte(o.Config), 0o600); err != nil {
		return nil, err
	}
	s := &Server{Password: o.Password, log: &syncBuffer{}, done: make(chan struct{})}
	if s.Password == "" {
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		s.Password = "E2e-" + hex.EncodeToString(b) + "-Pw1"
	}
	s.cmd = exec.Command(bin, "server", "--config", path)
	s.cmd.Env = append(append(os.Environ(), "WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD="+s.Password), o.Env...)
	s.cmd.Stdout, s.cmd.Stderr = s.log, s.log
	if err := s.cmd.Start(); err != nil {
		return nil, err
	}
	go func() { s.err = s.cmd.Wait(); close(s.done) }()
	client := o.Client
	if client == nil {
		client = &http.Client{}
	}
	poll := *client
	poll.Timeout = 2 * time.Second // a server that accepts but never answers
	deadline := time.Now().Add(30 * time.Second)
	for {
		select {
		case <-s.done:
			return nil, fmt.Errorf("the server exited before it answered (%v):\n%s", s.err, s.Log())
		default:
		}
		if code, _, _, err := Request(&poll, http.MethodGet, o.BaseURL+"/api/v1/auth/me", "", "admin", s.Password, false); err == nil && code == http.StatusOK {
			return s, nil
		}
		if time.Now().After(deadline) {
			s.Kill()
			return nil, fmt.Errorf("the server did not answer %s within 30 seconds:\n%s", o.BaseURL, s.Log())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Stop sends SIGTERM, waits for the server to stop, and returns its exit
// code: -1 when a signal ended it, which includes the SIGKILL Stop sends
// when the server has not stopped within 30 seconds.
func (s *Server) Stop() int {
	_ = s.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-s.done:
	case <-time.After(30 * time.Second):
		s.Kill()
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
