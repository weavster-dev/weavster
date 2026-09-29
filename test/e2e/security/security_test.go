// Package security is the black-box security suite: it runs the weavster
// binary as its own process and checks, over HTTP and HTTPS only, that the
// server refuses what it must (missing or wrong credentials, locked
// accounts, revoked tokens, missing CSRF markers, missing permissions, weak
// TLS) and sends its security headers.
package security

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/test/e2e/harness"
)

var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "weavster-e2e-security")
	if err != nil {
		panic(err)
	}
	if bin, err = harness.Build(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// start runs a server on a free address with extra configuration and
// returns its base URL and admin password; it is stopped (and must exit 0)
// when the test ends.
func start(t *testing.T, extra string) (string, string) {
	t.Helper()
	addr, err := harness.FreeAddr()
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + addr
	s, err := harness.Start(bin, t.TempDir(), harness.Options{Config: "listen: {address: \"" + addr + "\"}\n" + extra, BaseURL: base})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if code := s.Stop(); code != 0 {
			t.Errorf("SIGTERM: exit %d\n%s", code, s.Log())
		}
	})
	return base, s.Password
}

func TestAuthentication(t *testing.T) {
	base, adminPassword := start(t, "auth: {lockout: {retryLimit: 3, lockoutPeriodSeconds: 300}}\n")
	c := http.DefaultClient
	// req is one request; it fails t, the (sub)test it is called for.
	req := func(t *testing.T, method, path, body, user, password string, noCSRF bool) (int, string, http.Header) {
		t.Helper()
		code, resp, header, err := harness.Request(c, method, base+path, body, user, password, noCSRF)
		if err != nil {
			t.Fatal(err)
		}
		return code, resp, header
	}
	const admin, ops, opsPassword = "admin", "ops", "Ops-Password-1"
	if code, body, _ := req(t, http.MethodPost, "/api/v1/users", `{"username":"ops","password":"`+opsPassword+`","permissions":["flows:view"],"mustChangePassword":false}`, admin, adminPassword, false); code != http.StatusCreated {
		t.Fatalf("create ops: %d %s", code, body)
	}

	cases := []struct {
		name, method, path, body, user, password string
		noCSRF                                   bool
		want                                     int
	}{
		{"the API description needs nothing", http.MethodGet, "/api/openapi.yaml", "", "", "", true, http.StatusOK},
		{"no credentials", http.MethodGet, "/api/v1/flows", "", "", "", false, http.StatusUnauthorized},
		{"metrics need credentials", http.MethodGet, "/metrics", "", "", "", false, http.StatusUnauthorized},
		{"unknown user", http.MethodGet, "/api/v1/flows", "", "nobody", "Whatever-Pass-1", false, http.StatusUnauthorized},
		{"right credentials", http.MethodGet, "/api/v1/flows", "", admin, adminPassword, false, http.StatusOK},
		{"no CSRF marker", http.MethodGet, "/api/v1/flows", "", admin, adminPassword, true, http.StatusBadRequest},
		{"no CSRF marker on a change", http.MethodPost, "/api/v1/flows", `{"id":"x"}`, admin, adminPassword, true, http.StatusBadRequest},
		{"a permission the user has", http.MethodGet, "/api/v1/flows", "", ops, opsPassword, false, http.StatusOK},
		{"a permission the user lacks", http.MethodPost, "/api/v1/flows", `{"id":"x"}`, ops, opsPassword, false, http.StatusForbidden},
		{"users:admin the user lacks", http.MethodGet, "/api/v1/users", "", ops, opsPassword, false, http.StatusForbidden},
		{"TRACE is refused", http.MethodTrace, "/api/v1/flows", "", admin, adminPassword, false, http.StatusMethodNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body, header := req(t, tc.method, tc.path, tc.body, tc.user, tc.password, tc.noCSRF)
			if code != tc.want {
				t.Errorf("%s %s: %d %s, want %d", tc.method, tc.path, code, body, tc.want)
			}
			for name, want := range map[string]string{
				"X-Frame-Options":         "DENY",
				"X-Content-Type-Options":  "nosniff",
				"Content-Security-Policy": "frame-ancestors 'none'",
			} {
				if got := header.Get(name); !strings.Contains(got, want) {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
		})
	}

	t.Run("lockout", func(t *testing.T) {
		_, unknown, _ := req(t, http.MethodGet, "/api/v1/flows", "", "nobody", "Wrong-Password-1", false)
		var wrong string
		for i := 0; i < 3; i++ {
			code, body, _ := req(t, http.MethodGet, "/api/v1/flows", "", ops, "Wrong-Password-1", false)
			if code != http.StatusUnauthorized {
				t.Fatalf("wrong password %d: %d", i+1, code)
			}
			wrong = body
		}
		code, locked, _ := req(t, http.MethodGet, "/api/v1/flows", "", ops, opsPassword, false)
		if code != http.StatusUnauthorized {
			t.Errorf("the right password after the lockout: %d %s, want 401", code, locked)
		}
		// Anti-enumeration: the answer does not tell an unknown user, a
		// wrong password, and a locked account apart.
		if unknown != wrong || wrong != locked {
			t.Errorf("login failures differ:\nunknown user: %s\nwrong password: %s\nlocked: %s", unknown, wrong, locked)
		}
		if code, body, _ := req(t, http.MethodGet, "/api/v1/flows", "", admin, adminPassword, false); code != http.StatusOK {
			t.Errorf("another account is not locked: %d %s", code, body)
		}
	})

	t.Run("bearer token", func(t *testing.T) {
		code, body, _ := req(t, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"`+adminPassword+`"}`, "", "", false)
		var login struct{ Token string }
		if code != http.StatusOK || json.Unmarshal([]byte(body), &login) != nil || login.Token == "" {
			t.Fatalf("login: %d %s", code, body)
		}
		bearer := func(method, path string) int {
			r, _ := http.NewRequest(method, base+path, nil)
			r.Header.Set("X-Weavster-CSRF", "1")
			r.Header.Set("Authorization", "Bearer "+login.Token)
			resp, err := c.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			return resp.StatusCode
		}
		if code := bearer(http.MethodGet, "/api/v1/auth/me"); code != http.StatusOK {
			t.Errorf("with the token: %d", code)
		}
		if code := bearer(http.MethodPost, "/api/v1/auth/logout"); code != http.StatusNoContent {
			t.Errorf("logout: %d", code)
		}
		if code := bearer(http.MethodGet, "/api/v1/auth/me"); code != http.StatusUnauthorized {
			t.Errorf("with the token after logout: %d, want 401", code)
		}
	})
}

func TestTLS(t *testing.T) {
	certs := harness.NewCertificates()
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "server.pem"), filepath.Join(dir, "server.key")
	if err := os.WriteFile(certFile, certs.Cert, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, certs.Key, 0o600); err != nil {
		t.Fatal(err)
	}
	addr, err := harness.FreeAddr()
	if err != nil {
		t.Fatal(err)
	}
	base := "https://" + addr
	client, err := harness.TLSClient(certs.CA, 0)
	if err != nil {
		t.Fatal(err)
	}
	config := "listen: {address: \"\", tlsAddress: \"" + addr + "\"}\n" +
		"tls: {certFile: \"" + certFile + "\", keyFile: \"" + keyFile + "\", minVersion: \"1.3\"}\n"
	s, err := harness.Start(bin, t.TempDir(), harness.Options{Config: config, BaseURL: base, Client: client})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if code := s.Stop(); code != 0 {
			t.Errorf("SIGTERM: exit %d\n%s", code, s.Log())
		}
	}()

	code, body, header, err := harness.Request(client, http.MethodGet, base+"/api/v1/system", "", "admin", s.Password, false)
	if err != nil || code != http.StatusOK || !strings.Contains(body, `"status":"running"`) {
		t.Errorf("over HTTPS with the private CA: %d %s %v", code, body, err)
	}
	if got := header.Get("Strict-Transport-Security"); !strings.Contains(got, "max-age=") {
		t.Errorf("Strict-Transport-Security = %q", got)
	}

	refused := []struct {
		name   string
		client *http.Client
	}{
		{"a client that does not trust the CA", &http.Client{}},
		{"a client below tls.minVersion", func() *http.Client {
			old, err := harness.TLSClient(certs.CA, tls.VersionTLS12)
			if err != nil {
				t.Fatal(err)
			}
			return old
		}()},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, _, err := harness.Request(tc.client, http.MethodGet, base+"/api/openapi.yaml", "", "", "", true); err == nil {
				t.Error("the TLS handshake succeeded")
			}
		})
	}
	t.Run("plain HTTP to the TLS port", func(t *testing.T) {
		// Go's TLS listener answers a plain HTTP request with 400 (or the
		// connection fails); never with the API.
		code, body, _, err := harness.Request(http.DefaultClient, http.MethodGet, "http://"+addr+"/api/openapi.yaml", "", "", "", true)
		if err == nil && (code != http.StatusBadRequest || strings.Contains(body, "openapi")) {
			t.Errorf("plain HTTP to the TLS port: %d %s, want 400 without the API", code, body)
		}
	})
}
