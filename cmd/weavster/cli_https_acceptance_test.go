package main

import (
	"bytes"
	"crypto/tls"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestCLIOverHTTPS: a server with HTTPS only, under a context path, is
// used from the CLI with its CA (-ca) and credentials; without the CA the
// CLI says how to trust the server; paths outside the context path are
// 404, and /metrics is under it too.
func TestCLIOverHTTPS(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile, pool := selfSignedCert(t, dir)
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \"\", tlsAddress: \""+addr+"\", contextPath: /weavster}\n"+
		"tls: {certFile: \""+certFile+"\", keyFile: \""+keyFile+"\"}\n"+storeConfig(t))

	stderr := &syncBuffer{}
	done := make(chan int, 1)
	go func() { done <- run([]string{"server", "--config", cfg}, strings.NewReader(""), io.Discard, stderr) }()
	exited := false
	defer func() {
		if exited {
			return
		}
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("the server did not stop")
		}
	}()
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}
	base := "https://" + addr + "/weavster"
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := client.Get(base + "/api/openapi.yaml")
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
			t.Fatalf("the server never served HTTPS under /weavster: %v (%s)", err, stderr.String())
		}
	}

	// Outside the context path: 404.
	resp, err := client.Get("https://" + addr + "/api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /api/openapi.yaml outside /weavster = %d, want 404", resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodGet, base+"/metrics", nil)
	req.SetBasicAuth(bootstrapAdmin, testAdminPassword)
	if resp, err := client.Do(req); err != nil || resp.StatusCode != http.StatusOK {
		t.Errorf("GET /weavster/metrics: %v %v", resp, err)
	} else {
		_ = resp.Body.Close()
	}

	// The CLI with the CA and credentials.
	script := filepath.Join(dir, "script.txt")
	if err := os.WriteFile(script, []byte("status\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"-a", base, "-ca", certFile, "-u", bootstrapAdmin, "-p", testAdminPassword, "-s", script}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("CLI over https with -ca: exit %d: %s %s", code, out.String(), errOut.String())
	}
	if strings.Contains(errOut.String(), "Warning") || strings.Contains(errOut.String(), "Could not log in") {
		t.Errorf("CLI stderr: %s", errOut.String())
	}

	// Without the CA: the CLI says how to trust the server.
	out.Reset()
	errOut.Reset()
	_ = run([]string{"-a", base, "-u", bootstrapAdmin, "-p", testAdminPassword, "-s", script}, strings.NewReader(""), &out, &errOut)
	if !strings.Contains(errOut.String(), "pass that CA with -ca FILE") {
		t.Errorf("CLI without -ca: %s", errOut.String())
	}
}
