package main

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// productionExample is the production configuration the docs publish.
const productionExample = "../../docs/examples/production/weavster-server.yaml"

// TestProductionExample: the published production configuration is valid
// and secure as documented (HTTPS only with TLS 1.3, PostgreSQL over
// verified TLS, the marker header, a strict login policy); with test
// certificates and SQLite in place of its paths and database, the server
// starts from it and serves only HTTPS, refusing TLS 1.2, requests without
// the marker header, and requests without credentials.
func TestProductionExample(t *testing.T) {
	cfg, err := serverconfig.Load(productionExample)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen.Address != "" || cfg.Listen.TLSAddress == "" || !cfg.Listen.RequireMarkerHeader || cfg.TLS.MinVersion != "1.3" ||
		cfg.Store.Dialect != serverconfig.DialectPostgres || !strings.Contains(cfg.Store.DSN, "sslmode=verify-full") || strings.Contains(cfg.Store.DSN, ":@") ||
		cfg.Auth.PasswordPolicy.MinLength < 12 || cfg.Auth.Lockout.RetryLimit < 1 {
		t.Fatalf("the production example is not what docs/production.md says: %+v", cfg)
	}

	raw, err := os.ReadFile(productionExample)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile, pool := selfSignedCert(t, t.TempDir())
	addr := freeAddr(t)
	local := strings.NewReplacer(
		"tlsAddress: 0.0.0.0:8443", "tlsAddress: "+addr,
		"/etc/weavster/tls/server.crt", certFile,
		"/etc/weavster/tls/server.key", keyFile,
		"dialect: postgres", "dialect: sqlite",
		"dsn: postgres://weavster@db.internal:5432/weavster?sslmode=verify-full", "dsn: "+filepath.Join(t.TempDir(), "weavster.db"),
		"/var/lib/weavster", t.TempDir(),
	).Replace(string(raw))
	path := filepath.Join(t.TempDir(), "weavster-server.yaml")
	if err := os.WriteFile(path, []byte(local), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}
	// startCLI waits over plain HTTP; this server has only HTTPS.
	done := make(chan int, 1)
	go func() {
		done <- run([]string{"server", "--config", path}, strings.NewReader(""), io.Discard, io.Discard)
	}()
	defer func() {
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
		select {
		case <-done:
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
		if time.Now().After(deadline) {
			t.Fatalf("the server never served HTTPS: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	get := func(path string, marker bool, creds func(*http.Request)) int {
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
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	if code := get("/api/v1/flows", true, nil); code != http.StatusUnauthorized {
		t.Errorf("without credentials: %d, want 401", code)
	}
	if code := get("/api/v1/flows", false, basic(bootstrapAdmin, testAdminPassword)); code != http.StatusBadRequest {
		t.Errorf("without the marker header: %d, want 400", code)
	}
	if code := get("/api/v1/flows", true, basic(bootstrapAdmin, testAdminPassword)); code != http.StatusOK {
		t.Errorf("with both: %d, want 200", code)
	}
	old := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12}
	if conn, err := tls.Dial("tcp", addr, old); err == nil {
		_ = conn.Close()
		t.Error("a TLS 1.2 client was accepted; the example requires TLS 1.3")
	}
	if conn, err := net.DialTimeout("tcp", "127.0.0.1:8080", 200*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Log("something listens on 127.0.0.1:8080; the cleartext check is skipped")
	} else if code := func() int {
		resp, err := http.Get("http://" + addr + "/api/openapi.yaml")
		if err != nil {
			return 0
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}(); code == http.StatusOK {
		t.Error("the HTTPS port answered cleartext HTTP")
	}
}
