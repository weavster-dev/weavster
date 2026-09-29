package harness

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestHarness(t *testing.T) {
	bin, err := Build(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	addr, err := FreeAddr()
	if err != nil {
		t.Fatal(err)
	}
	config := "listen: {address: \"" + addr + "\"}\n"
	base := "http://" + addr

	t.Run("start and stop", func(t *testing.T) {
		s, err := Start(bin, t.TempDir(), config, base+"/api/openapi.yaml", nil)
		if err != nil {
			t.Fatal(err)
		}
		code, body, _, err := Request(http.DefaultClient, http.MethodGet, base+"/api/v1/auth/me", "", "admin", AdminPassword, false)
		if err != nil || code != http.StatusOK || !strings.Contains(body, `"username":"admin"`) {
			t.Errorf("signed-in request: %d %s %v", code, body, err)
		}
		if code, _, _, _ := Request(http.DefaultClient, http.MethodPost, base+"/api/v1/auth/logout", `{}`, "", "", true); code == 0 {
			t.Error("a request without credentials or marker got no answer")
		}
		if code := s.Stop(); code != 0 {
			t.Errorf("SIGTERM: exit %d\n%s", code, s.Log())
		}
	})
	t.Run("killed", func(t *testing.T) {
		s, err := Start(bin, t.TempDir(), config, base+"/api/openapi.yaml", nil, "WEAVSTER_E2E=1")
		if err != nil {
			t.Fatal(err)
		}
		s.Kill()
		if code := s.Stop(); code != -1 {
			t.Errorf("a killed server exited %d, want -1 (a signal)", code)
		}
	})
	t.Run("exits before it answers", func(t *testing.T) {
		if _, err := Start(bin, t.TempDir(), "listen: {address: \"\"}\n", base+"/api/openapi.yaml", nil); err == nil ||
			!strings.Contains(err.Error(), "listen.address or listen.tlsAddress is required") {
			t.Errorf("err = %v, want the server's error", err)
		}
	})
	t.Run("errors", func(t *testing.T) {
		if _, err := Build(filepath.Join(t.TempDir(), "missing", "\x00")); err == nil {
			t.Error("Build into an invalid path succeeded")
		}
		if _, err := Start(bin, filepath.Join(t.TempDir(), "missing"), config, base, nil); err == nil {
			t.Error("Start with no directory for the config succeeded")
		}
		if _, err := Start(filepath.Join(t.TempDir(), "missing"), t.TempDir(), config, base, nil); err == nil {
			t.Error("Start of a missing binary succeeded")
		}
		if _, _, _, err := Request(http.DefaultClient, "BAD METHOD", base, "", "", "", false); err == nil {
			t.Error("Request with an invalid method succeeded")
		}
		if _, _, _, err := Request(http.DefaultClient, http.MethodGet, "http://"+addr, "", "", "", false); err == nil {
			t.Error("Request to a stopped server succeeded")
		}
		if _, err := TLSClient([]byte("not PEM"), 0); err == nil {
			t.Error("TLSClient without a CA succeeded")
		}
	})
}

func TestCertificates(t *testing.T) {
	c := NewCertificates()
	pair, err := tls.X509KeyPair(c.Cert, c.Key)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{pair}}
	srv.StartTLS()
	defer srv.Close()
	client, err := TLSClient(c.CA, 0)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("the CA does not verify the server certificate: %v", err)
	}
	_ = resp.Body.Close()
}
