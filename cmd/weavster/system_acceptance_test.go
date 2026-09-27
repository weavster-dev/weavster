package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestSystemInfo: /api/v1/system is computed per request and reports TLS as
// configured; about, password requirements, resources, and GUIDs are served
// to any signed-in user.
func TestSystemInfo(t *testing.T) {
	cfg := serverconfig.Default()
	cfg.Listen.TLSAddress, cfg.TLS.MinVersion = "127.0.0.1:8443", "1.3"
	cfg.Auth.PasswordPolicy = serverconfig.PasswordPolicy{MinLength: 12, MinUpper: 1, MinLower: 2, MinNumeric: 1, MinSpecial: 2}
	c := startComposed(t, cfg, io.Discard)
	viewer := basic(bootstrapAdmin, testAdminPassword)
	get := func(path string) string {
		t.Helper()
		code, body, _ := c.do(http.MethodGet, path, "", viewer)
		if code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, code, body)
		}
		return body
	}
	var first, second struct {
		Time string
		TLS  struct {
			Enabled    bool
			MinVersion string
			Protocols  []string
			Ciphers    []string
		}
	}
	_ = json.Unmarshal([]byte(get("/api/v1/system")), &first)
	time.Sleep(1100 * time.Millisecond)
	_ = json.Unmarshal([]byte(get("/api/v1/system")), &second)
	if first.Time == second.Time {
		t.Errorf("time is not computed per request: %s", first.Time)
	}
	if !first.TLS.Enabled || first.TLS.MinVersion != "1.3" || strings.Join(first.TLS.Protocols, ",") != "TLS 1.3" || len(first.TLS.Ciphers) != 3 {
		t.Errorf("tls = %+v", first.TLS)
	}
	if body := get("/api/v1/system/password-requirements"); !strings.Contains(body, `"rules":["at least 12 characters","at least 1 uppercase letter","at least 2 lowercase letters","at least 1 digit","at least 2 special characters"]`) {
		t.Errorf("password requirements = %s", body)
	}
	for path, want := range map[string]string{
		"/api/v1/system/about": `"name":"Weavster"`, "/api/v1/system/resources": `"goroutines":`, "/api/v1/system/guid": `"guid":"`,
	} {
		if body := get(path); !strings.Contains(body, want) {
			t.Errorf("%s = %s", path, body)
		}
	}

	// Without TLS, nothing is claimed.
	plain := startComposed(t, serverconfig.Default(), io.Discard)
	if _, body, _ := plain.do(http.MethodGet, "/api/v1/system", "", viewer); !strings.Contains(body, `"tls":{"enabled":false,"protocols":[],"ciphers":[]}`) {
		t.Errorf("no TLS = %s", body)
	}
	// TLS 1.2 also lists the 1.2 suites.
	cfg.TLS.MinVersion = "1.2"
	tls12 := startComposed(t, cfg, io.Discard)
	if _, body, _ := tls12.do(http.MethodGet, "/api/v1/system", "", viewer); !strings.Contains(body, `"protocols":["TLS 1.2","TLS 1.3"]`) || !strings.Contains(body, "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256") {
		t.Errorf("TLS 1.2 = %s", body)
	}
}

// TestPasswordRulesForbidden: -1 forbids a class, and 0 adds no rule.
func TestPasswordRulesForbidden(t *testing.T) {
	cfg := serverconfig.Default()
	cfg.Auth.PasswordPolicy = serverconfig.PasswordPolicy{MinSpecial: -1}
	if got := (systemAdapter{cfg: cfg}).PasswordRequirements().Rules; strings.Join(got, ",") != "no special characters" {
		t.Errorf("rules = %v", got)
	}
}
