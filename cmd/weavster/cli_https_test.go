package main

import (
	"bytes"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlainCredentials(t *testing.T) {
	for addr, want := range map[string]bool{
		"http://127.0.0.1:8080":          false,
		"http://localhost:8080":          false,
		"http://[::1]:8080":              false,
		"http://weavster.internal:8080":  true,
		"http://10.0.0.5:8080":           true,
		"https://weavster.internal:8443": false,
		"://bad":                         false,
	} {
		if got := plainCredentials(addr); got != want {
			t.Errorf("plainCredentials(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestWithCA(t *testing.T) {
	dir := t.TempDir()
	notPEM := filepath.Join(dir, "not.pem")
	if err := os.WriteFile(notPEM, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	certFile, _, _ := selfSignedCert(t, dir)
	for _, tt := range []struct{ name, file, wantErr string }{
		{"system roots only", "", ""},
		{"a CA file", certFile, ""},
		{"missing file", filepath.Join(dir, "none.pem"), "CA file:"},
		{"not PEM", notPEM, "holds no PEM certificate"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := newHTTPClient("", "", "").withCA(tt.file)
			if (tt.wantErr == "" && err != nil) || (tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr))) {
				t.Errorf("withCA(%q) = %v, want %q", tt.file, err, tt.wantErr)
			}
		})
	}
}

// TestCertificateHints: a CLI error caused by the server's certificate
// says what to do about it.
func TestCertificateHints(t *testing.T) {
	for _, tt := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("get: %w", x509.UnknownAuthorityError{}), "pass that CA with -ca FILE"},
		{fmt.Errorf("get: %w", x509.CertificateInvalidError{Reason: x509.Expired}), "has expired or is not valid yet"},
		{fmt.Errorf("get: %w", x509.CertificateInvalidError{Reason: x509.IncompatibleUsage}), "the error says why"},
		{fmt.Errorf("get: %w", x509.HostnameError{Certificate: &x509.Certificate{}, Host: "other"}), "issued for another name"},
		{errors.New("connection refused"), ""},
	} {
		var out bytes.Buffer
		shellError(&out, false, tt.err)
		if tt.want == "" && strings.Count(out.String(), "\n") != 1 || tt.want != "" && !strings.Contains(out.String(), tt.want) {
			t.Errorf("shellError(%v) = %q, want %q", tt.err, out.String(), tt.want)
		}
	}
}

// TestConnectionCA: ca in the connection file, overridden by -ca.
func TestConnectionCA(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conn.yaml")
	if err := os.WriteFile(path, []byte("address: https://weavster.internal:8443\nca: /etc/weavster/ca.pem\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := loadConnection(path)
	if err != nil || c.CA != "/etc/weavster/ca.pem" {
		t.Fatalf("loadConnection = %+v %v", c, err)
	}
	if got := c.override(connection{CA: "/tmp/other.pem"}); got.CA != "/tmp/other.pem" {
		t.Errorf("-ca did not override: %+v", got)
	}
	rel := filepath.Join(t.TempDir(), "conn.yaml")
	if err := os.WriteFile(rel, []byte("ca: ca.pem\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if c, err := loadConnection(rel); err != nil || c.CA != filepath.Join(filepath.Dir(rel), "ca.pem") {
		t.Errorf("relative ca = %q %v, want it next to the file", c.CA, err)
	}
}

// TestCLIConnectionWarnings: credentials for a plain-HTTP address on
// another host draw a warning; an unreadable -ca file stops the CLI.
func TestCLIConnectionWarnings(t *testing.T) {
	var out, errOut bytes.Buffer
	_ = run([]string{"-a", "http://weavster.invalid:1", "-u", "a", "-p", "b", "-v"}, strings.NewReader(""), &out, &errOut)
	if !strings.Contains(errOut.String(), "Warning: http://weavster.invalid:1 is plain HTTP") {
		t.Errorf("stderr = %s", errOut.String())
	}
	errOut.Reset()
	certFile, _, _ := selfSignedCert(t, t.TempDir())
	_ = run([]string{"-a", "http://127.0.0.1:1", "-ca", certFile, "-v"}, strings.NewReader(""), &out, &errOut)
	if !strings.Contains(errOut.String(), "Warning: -ca is used only for https addresses") {
		t.Errorf("-ca with http: %s", errOut.String())
	}
	errOut.Reset()
	if code := run([]string{"-a", "https://weavster.invalid:1", "-ca", filepath.Join(t.TempDir(), "none.pem"), "-v"}, strings.NewReader(""), &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "CA file:") {
		t.Errorf("bad -ca: exit %d, %s", code, errOut.String())
	}
}
