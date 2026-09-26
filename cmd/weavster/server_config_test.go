package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/gateway"
)

// freeAddr returns a currently unused loopback address.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func writeConfig(t *testing.T, yaml string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "weavster.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func apiGet(t *testing.T, client *http.Client, url string, marker bool) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if marker {
		req.Header.Set(gateway.MarkerHeader, gateway.MarkerValue)
	}
	req.SetBasicAuth(bootstrapAdmin, testAdminPassword)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// TestServerConfigStore proves store.dialect selection through
// `weavster server --config`.
func TestServerConfigStore(t *testing.T) {
	tests := []struct {
		name       string
		store      string
		wantStatus int
		wantBody   string
		wantFile   string
	}{
		{name: "memory", store: "dialect: memory", wantStatus: http.StatusOK, wantBody: "[]"},
		{name: "sqlite-default-dsn", store: "dialect: sqlite", wantStatus: http.StatusOK, wantBody: "[]", wantFile: "weavster.db"},
		{name: "disabled", store: "dialect: disabled", wantStatus: http.StatusServiceUnavailable, wantBody: "messages unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataDir := filepath.Join(t.TempDir(), "data")
			addr := freeAddr(t)
			cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {"+tt.store+"}\npaths: {dataDir: \""+dataDir+"\"}\n")
			stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
			defer stop()

			status, body := apiGet(t, http.DefaultClient, "http://"+addr+"/api/v1/messages", true)
			if status != tt.wantStatus || !strings.Contains(body, tt.wantBody) {
				t.Errorf("GET /api/v1/messages = %d %q, want %d containing %q", status, body, tt.wantStatus, tt.wantBody)
			}
			if tt.wantFile != "" {
				if _, err := os.Stat(filepath.Join(dataDir, tt.wantFile)); err != nil {
					t.Errorf("store file not created: %v", err)
				}
			}
		})
	}
}

// TestServerConfigListen proves listen.requireMarkerHeader and the
// positional address override.
func TestServerConfigListen(t *testing.T) {
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \"127.0.0.1:1\", requireMarkerHeader: false}\n")
	stop := startCLI(t, []string{"server", "--config", cfg, addr}, "http://"+addr+"/api/openapi.yaml")
	defer stop()

	if status, _ := apiGet(t, http.DefaultClient, "http://"+addr+"/api/v1/system", false); status != http.StatusOK {
		t.Errorf("GET /api/v1/system without marker = %d, want 200", status)
	}
}

// TestServerConfigTLS proves the HTTPS listener and tls.minVersion.
func TestServerConfigTLS(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile, pool := selfSignedCert(t, dir)
	addr, tlsAddr := freeAddr(t), freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\", tlsAddress: \""+tlsAddr+"\"}\n"+
		"tls: {certFile: \""+certFile+"\", keyFile: \""+keyFile+"\", minVersion: \"1.3\"}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
	if status, _ := apiGet(t, client, "https://"+tlsAddr+"/api/v1/system", true); status != http.StatusOK {
		t.Errorf("HTTPS GET /api/v1/system = %d, want 200", status)
	}

	old := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MaxVersion: tls.VersionTLS12}}}
	if resp, err := old.Get("https://" + tlsAddr + "/api/openapi.yaml"); err == nil {
		_ = resp.Body.Close()
		t.Error("TLS 1.2 client connected despite minVersion 1.3")
	}
}

// TestServerConfigErrors proves invalid configuration and store connection
// failures stop startup with exit 1 and an Error: message.
func TestServerConfigErrors(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		args func(t *testing.T) []string
		want string
	}{
		{name: "missing-file", args: func(t *testing.T) []string {
			return []string{"server", "--config", filepath.Join(t.TempDir(), "absent.yaml")}
		}, want: "Error: config:"},
		{name: "unknown-key", args: func(t *testing.T) []string {
			return []string{"server", "--config", writeConfig(t, "listen: {adress: x}\n")}
		}, want: "field adress not found"},
		{name: "invalid-dialect", args: func(t *testing.T) []string {
			return []string{"server", "--config", writeConfig(t, "store: {dialect: oracle}\n")}
		}, want: "store.dialect must be"},
		{name: "unknown-flag", args: func(*testing.T) []string {
			return []string{"server", "--nope"}
		}, want: "flag provided but not defined"},
		{name: "retry-exhausted", args: func(t *testing.T) []string {
			return []string{"server", "--config", writeConfig(t,
				"store: {dialect: postgres, dsn: \"postgres://u:p@127.0.0.1:1/db?connect_timeout=1\", maxRetry: 2, retryWaitMs: 10}\n")}
		}, want: "giving up after 3 attempts"},
		{name: "sqlite-dir-uncreatable", args: func(t *testing.T) []string {
			return []string{"server", "--config", writeConfig(t, "store: {dialect: sqlite}\npaths: {dataDir: \""+filepath.Join(blocker, "sub")+"\"}\n")}
		}, want: "Error: store:"},
		{name: "sqlite-not-retried", args: func(t *testing.T) []string {
			return []string{"server", "--config", writeConfig(t, "store: {dialect: sqlite, dsn: \""+t.TempDir()+"\", maxRetry: 5, retryWaitMs: 60000}\n")}
		}, want: "Error: store: sqlite:"},
		{name: "extra-arguments", args: func(*testing.T) []string {
			return []string{"server", "127.0.0.1:0", "--config", "weavster.yaml"}
		}, want: "unexpected arguments"},
		{name: "unreadable-tls-cert", args: func(t *testing.T) []string {
			dir := t.TempDir()
			return []string{"server", "--config", writeConfig(t, "listen: {tlsAddress: \"127.0.0.1:0\"}\ntls: {certFile: \""+
				filepath.Join(dir, "cert.pem")+"\", keyFile: \""+filepath.Join(dir, "key.pem")+"\"}\n")}
		}, want: "Error: tls:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			if code := run(tt.args(t), strings.NewReader(""), &out, &errb); code != 1 {
				t.Fatalf("exit = %d, want 1 (stderr %q)", code, errb.String())
			}
			if !strings.Contains(errb.String(), tt.want) {
				t.Errorf("stderr %q does not contain %q", errb.String(), tt.want)
			}
		})
	}
}

// TestServerStopDuringStoreRetry proves SIGTERM interrupts store connection
// retries and exits cleanly.
func TestServerStopDuringStoreRetry(t *testing.T) {
	cfg := writeConfig(t, "store: {dialect: postgres, dsn: \"postgres://u:p@127.0.0.1:1/db?connect_timeout=1\", maxRetry: 1000, retryWaitMs: 50}\n")
	done := make(chan int, 1)
	errb := &syncBuffer{}
	go func() { done <- run([]string{"server", "--config", cfg}, strings.NewReader(""), io.Discard, errb) }()

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(errb.String(), "store connection failed") {
		if time.Now().After(deadline) {
			t.Fatalf("no retry logged: %q", errb.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit = %d, want 0 (stderr %q)", code, errb.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SIGTERM did not interrupt store retries")
	}
}

func TestServerHelpFlag(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"server", "-h"}, strings.NewReader(""), &out, &errb); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(errb.String(), "-config") {
		t.Errorf("usage %q does not mention -config", errb.String())
	}
}

// selfSignedCert writes a 127.0.0.1 certificate and key into dir.
func selfSignedCert(t *testing.T, dir string) (certFile, keyFile string, pool *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool = x509.NewCertPool()
	pool.AddCert(cert)
	return certFile, keyFile, pool
}

// TestFlowsSurviveRestart proves flow definitions are durable in the sqlite
// store: a flow created through the API is still there after a restart.
func TestFlowsSurviveRestart(t *testing.T) {
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n")
	base := "http://" + addr
	c := apiClient{t: t, base: base}
	admin := basic(bootstrapAdmin, testAdminPassword)

	stop := startCLI(t, []string{"server", "--config", cfg}, base+"/api/openapi.yaml")
	if status, body, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"lab","name":"Lab Results","sourceType":"http"}`, admin); status != http.StatusCreated {
		t.Fatalf("create: %d %q", status, body)
	}
	stop()

	stop = startCLI(t, []string{"server", "--config", cfg}, base+"/api/openapi.yaml")
	defer stop()
	if status, body, _ := c.do(http.MethodGet, "/api/v1/flows/lab", "", admin); status != http.StatusOK || !strings.Contains(body, "Lab Results") {
		t.Errorf("after restart: %d %q, want the lab flow", status, body)
	}
	if status, body, _ := c.do(http.MethodGet, "/api/v1/topology", "", admin); status != http.StatusOK || !strings.Contains(body, "flow:lab") {
		t.Errorf("topology after restart: %d %q", status, body)
	}
}
