package main

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// productionExample is the production configuration the docs publish.
const productionExample = "../../docs/examples/production/weavster-server.yaml"

// TestProductionExample: the published production configuration is valid,
// secure as documented (HTTPS only with TLS 1.3, a durable store, the
// marker header, a strict login policy), and shown verbatim on the
// Production setup page; with test certificates, a free port, and a
// temporary data directory in place of its own, the server starts from it, listens only on its HTTPS port, and refuses TLS 1.2,
// requests without the marker header, and requests without credentials.
func TestProductionExample(t *testing.T) {
	raw, err := os.ReadFile(productionExample)
	if err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile("../../docs/production.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "```yaml\n"+string(raw)+"```") {
		t.Error("docs/production.md does not show docs/examples/production/weavster-server.yaml verbatim")
	}
	cfg, err := serverconfig.Load(productionExample)
	if err == nil {
		err = cfg.Validate() // Load decodes; the server validates before it starts
	}
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen.Address != "" || cfg.Listen.TLSAddress == "" || !cfg.Listen.RequireMarkerHeader || cfg.TLS.MinVersion != "1.3" ||
		cfg.Store.Dialect != serverconfig.DialectSQLite || cfg.Store.DSN != "" || cfg.Paths.DataDir == "" ||
		cfg.Auth.PasswordPolicy.MinLength < 12 || cfg.Auth.Lockout.RetryLimit < 1 {
		t.Fatalf("the production example is not what docs/production.md says: %+v", cfg)
	}

	// The same configuration with test certificates, a free port, and SQLite.
	certFile, keyFile, pool := selfSignedCert(t, t.TempDir())
	addr := freeAddr(t)
	cfg.Listen.TLSAddress = addr
	cfg.TLS.CertFile, cfg.TLS.KeyFile = certFile, keyFile
	cfg.Paths.DataDir = t.TempDir() // the store file goes here
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	local, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "weavster-server.yaml")
	if err := os.WriteFile(path, local, 0o600); err != nil {
		t.Fatal(err)
	}

	// startCLI waits over plain HTTP; this server has only HTTPS.
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}
	stderr := &syncBuffer{}
	done := make(chan int, 1)
	go func() { done <- run([]string{"server", "--config", path}, strings.NewReader(""), io.Discard, stderr) }()
	exited := false
	defer func() {
		if exited {
			return // no SIGTERM: without the server's handler it would end the test binary
		}
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
		select {
		case code := <-done:
			if code != 0 {
				t.Errorf("exit %d after SIGTERM: %s", code, stderr.String())
			}
		case <-time.After(5 * time.Second):
			t.Error("the server did not stop")
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := client.Get("https://" + addr + "/api/openapi.yaml")
		if err == nil {
			_ = resp.Body.Close()
			break
		}
		select {
		case code := <-done:
			exited = true
			t.Fatalf("the server exited %d: %s", code, stderr.String())
		case <-time.After(20 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatalf("the server never served HTTPS: %v (%s)", err, stderr.String())
		}
	}

	do := func(path string, marker bool, creds func(*http.Request)) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, "https://"+addr+path, nil)
		if marker {
			req.Header.Set("X-Weavster-CSRF", "1")
		}
		if creds != nil {
			creds(req)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	admin := basic(bootstrapAdmin, testAdminPassword)
	if code, _ := do("/api/v1/flows", true, nil); code != http.StatusUnauthorized {
		t.Errorf("without credentials: %d, want 401", code)
	}
	if code, _ := do("/api/v1/flows", false, admin); code != http.StatusBadRequest {
		t.Errorf("without the marker header: %d, want 400", code)
	}
	// The server's own listeners: only the HTTPS port.
	code, body := do("/api/v1/flows/ports-in-use", true, admin)
	var ports []struct {
		Port   int
		UsedBy string
	}
	_, portText, _ := net.SplitHostPort(addr)
	tlsPort, _ := strconv.Atoi(portText)
	if code != http.StatusOK || json.Unmarshal([]byte(body), &ports) != nil || len(ports) != 1 || ports[0].Port != tlsPort {
		t.Errorf("listeners: %d %s, want only the HTTPS port %d", code, body, tlsPort)
	}
	old := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12}
	if conn, err := tls.Dial("tcp", addr, old); err == nil {
		_ = conn.Close()
		t.Error("a TLS 1.2 client was accepted; the example requires TLS 1.3")
	}
}
