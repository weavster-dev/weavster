package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/auth"
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
	var status struct {
		Time          string
		UptimeSeconds *int64
		TLS           struct {
			Enabled    bool
			MinVersion string
			Protocols  []string
			Ciphers    []string
		}
	}
	before := time.Now().Add(-time.Second)
	if err := json.Unmarshal([]byte(get("/api/v1/system")), &status); err != nil {
		t.Fatal(err)
	}
	if at, err := time.Parse(time.RFC3339, status.Time); err != nil || at.Before(before.Truncate(time.Second)) || status.UptimeSeconds == nil {
		t.Errorf("time %q (%v) is not the request time; uptime %v", status.Time, err, status.UptimeSeconds)
	}
	first := status
	if !first.TLS.Enabled || first.TLS.MinVersion != "1.3" || strings.Join(first.TLS.Protocols, ",") != "TLS 1.3" || len(first.TLS.Ciphers) != 3 {
		t.Errorf("tls = %+v", first.TLS)
	}
	if body := get("/api/v1/system/password-requirements"); !strings.Contains(body, `"minLength":12`) || !strings.Contains(body, `"at least 2 lowercase letters"`) {
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
	// TLS 1.2 is listed; its suites depend on the certificate
	// (TestSystemTLSMatchesListener checks them over the real listener).
	cfg.TLS.MinVersion = "1.2"
	tls12 := startComposed(t, cfg, io.Discard)
	if _, body, _ := tls12.do(http.MethodGet, "/api/v1/system", "", viewer); !strings.Contains(body, `"protocols":["TLS 1.2","TLS 1.3"]`) {
		t.Errorf("TLS 1.2 = %s", body)
	}
}

// TestSystemAdapter covers the password rules for every count, and the
// TLS report for each certificate key type.
func TestSystemAdapter(t *testing.T) {
	for _, tt := range []struct {
		policy auth.PasswordPolicy
		want   string
	}{
		{auth.PasswordPolicy{}, ""},
		{auth.PasswordPolicy{MinLength: 8, MinUpper: 1, MinLower: 2, MinNumeric: 1, MinSpecial: 3},
			"at least 8 characters|at least 1 uppercase letter|at least 2 lowercase letters|at least 1 digit|at least 3 special characters (anything but letters and digits, spaces included)"},
		{auth.PasswordPolicy{MinUpper: -1, MinLower: -1, MinNumeric: -1, MinSpecial: -1},
			"no uppercase letters|no lowercase letters|no digits|no special characters (only letters and digits)"},
		{auth.PasswordPolicy{MinSpecial: 1}, "at least 1 special character (anything but a letter or digit, spaces included)"},
	} {
		if got := strings.Join((systemAdapter{policy: tt.policy}).PasswordRequirements().Rules, "|"); got != tt.want {
			t.Errorf("%+v: rules %q, want %q", tt.policy, got, tt.want)
		}
	}
	cfg := serverconfig.Default()
	cfg.Listen.TLSAddress = "127.0.0.1:8443"
	for _, tt := range []struct {
		key, want string
		n         int
	}{
		{"rsa", "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", 5},
		{"ecdsa", "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256", 5},
		{"", "TLS_AES_128_GCM_SHA256", 3}, // unknown key: no 1.2 suite claimed
	} {
		st := (systemAdapter{cfg: cfg, certKey: tt.key}).tlsStatus()
		if len(st.Ciphers) != tt.n || !strings.Contains(strings.Join(st.Ciphers, ","), tt.want) {
			t.Errorf("key %q: ciphers %v", tt.key, st.Ciphers)
		}
	}
	s := systemAdapter{started: time.Now().Add(-time.Minute)}
	if st := s.Status(); st.UptimeSeconds < 60 || st.Timezone == "Local" || !strings.Contains(st.Timezone, ":") {
		t.Errorf("status = %+v", st)
	}
	if r := s.Resources(); r.CPUs < 1 || r.MemorySysBytes == 0 || r.MemoryAllocBytes == 0 || r.UptimeSeconds < 60 {
		t.Errorf("resources = %+v", r)
	}
}
