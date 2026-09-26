package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/auth"
	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/serverconfig"
	"github.com/weavster-dev/weavster/internal/state"
)

// protectedRoutes lists every authenticated /api/v1 route with the
// permission it requires ("" = any authenticated user).
var protectedRoutes = []struct {
	method, path, perm string
}{
	{http.MethodGet, "/api/v1/system", ""},
	{http.MethodGet, "/api/v1/auth/me", ""},
	{http.MethodGet, "/api/v1/topology", "flows:view"},
	{http.MethodGet, "/api/v1/topology/flows/admit", "flows:view"},
	{http.MethodGet, "/api/v1/flows", "flows:view"},
	{http.MethodGet, "/api/v1/flows/admit", "flows:view"},
	{http.MethodPost, "/api/v1/flows", "flows:edit"},
	{http.MethodDelete, "/api/v1/flows/admit", "flows:edit"},
	{http.MethodGet, "/api/v1/messages", "messages:view"},
	{http.MethodPost, "/api/v1/flows/admit/messages", "messages:send"},
	{http.MethodGet, "/api/v1/flows/admit/stats", "flows:view"},
	{http.MethodGet, "/api/v1/events", "events:view"},
	{http.MethodPost, "/api/v1/flows/admit/deploy", "flows:deploy"},
	{http.MethodPost, "/api/v1/flows/redeploy-all", "flows:deploy"},
}

type apiClient struct {
	t    *testing.T
	base string
}

// do sends a marked API request with optional Basic or Bearer credentials.
func (c apiClient) do(method, path, body string, creds func(*http.Request)) (int, string, http.Header) {
	c.t.Helper()
	req, err := http.NewRequest(method, c.base+path, strings.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set(gateway.MarkerHeader, gateway.MarkerValue)
	if creds != nil {
		creds(req)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func basic(user, pass string) func(*http.Request) {
	return func(r *http.Request) { r.SetBasicAuth(user, pass) }
}

func bearer(token string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
}

func (c apiClient) login(user, pass string) (int, string) {
	c.t.Helper()
	body, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	status, resp, _ := c.do(http.MethodPost, "/api/v1/auth/login", string(body), nil)
	var out struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal([]byte(resp), &out)
	return status, out.Token
}

func startComposed(t *testing.T, cfg serverconfig.Config, out io.Writer) apiClient {
	t.Helper()
	handler, closeStore, err := buildServer(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), out, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(handler)
	t.Cleanup(func() { ts.Close(); _ = closeStore() })
	return apiClient{t: t, base: ts.URL}
}

// TestAuthRequired proves every protected route rejects missing and wrong
// credentials, and accepts Basic credentials.
func TestAuthRequired(t *testing.T) {
	cfg := serverconfig.Default()
	cfg.Auth.Lockout.RetryLimit = 0 // this test sends many wrong passwords
	c := startComposed(t, cfg, io.Discard)
	for _, rt := range protectedRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			status, body, hdr := c.do(rt.method, rt.path, "", nil)
			if status != http.StatusUnauthorized || !strings.Contains(body, `"UNAUTHORIZED"`) {
				t.Errorf("no credentials: %d %q, want 401 UNAUTHORIZED", status, body)
			}
			if hdr.Get("WWW-Authenticate") == "" {
				t.Error("missing WWW-Authenticate header")
			}
			if status, _, _ := c.do(rt.method, rt.path, "", basic(bootstrapAdmin, "wrong")); status != http.StatusUnauthorized {
				t.Errorf("wrong password: %d, want 401", status)
			}
			if status, _, _ := c.do(rt.method, rt.path, "", bearer("not-a-token")); status != http.StatusUnauthorized {
				t.Errorf("unknown token: %d, want 401", status)
			}
		})
	}
	if status, _, _ := c.do(http.MethodGet, "/api/v1/flows", "", basic(bootstrapAdmin, testAdminPassword)); status != http.StatusOK {
		t.Errorf("admin Basic: %d, want 200", status)
	}
}

// TestLoginLogout proves the token lifecycle: login, use, me, logout.
func TestLoginLogout(t *testing.T) {
	c := startComposed(t, serverconfig.Default(), io.Discard)

	for _, creds := range [][2]string{{bootstrapAdmin, "wrong"}, {"nobody", "wrong"}} {
		status, _, _ := c.do(http.MethodPost, "/api/v1/auth/login", `{"username":"`+creds[0]+`","password":"`+creds[1]+`"}`, nil)
		if status != http.StatusUnauthorized {
			t.Errorf("login %s: %d, want 401", creds[0], status)
		}
	}
	if status, _, _ := c.do(http.MethodPost, "/api/v1/auth/login", "not json", nil); status != http.StatusBadRequest {
		t.Errorf("malformed login: %d, want 400", status)
	}

	status, token := c.login(bootstrapAdmin, testAdminPassword)
	if status != http.StatusOK || token == "" {
		t.Fatalf("login: %d token=%q", status, token)
	}
	if status, body, _ := c.do(http.MethodGet, "/api/v1/auth/me", "", bearer(token)); status != http.StatusOK ||
		!strings.Contains(body, `"username":"admin"`) || !strings.Contains(body, `"admin"`) {
		t.Errorf("me: %d %q", status, body)
	}
	if status, _, _ := c.do(http.MethodGet, "/api/v1/flows", "", bearer(token)); status != http.StatusOK {
		t.Errorf("flows with token: %d, want 200", status)
	}
	if status, _, _ := c.do(http.MethodPost, "/api/v1/auth/logout", "", bearer(token)); status != http.StatusNoContent {
		t.Errorf("logout: %d, want 204", status)
	}
	if status, _, _ := c.do(http.MethodGet, "/api/v1/flows", "", bearer(token)); status != http.StatusUnauthorized {
		t.Errorf("flows after logout: %d, want 401", status)
	}
}

// TestPermissionMatrix proves each route enforces its permission, using the
// composition root's auth adapters with users holding one permission each.
func TestPermissionMatrix(t *testing.T) {
	provider := auth.NewLocalProvider(auth.Options{})
	// Usernames are the permission with ":" replaced, since Basic auth
	// usernames cannot contain a colon.
	users := map[string][]string{"none": nil, "flows-view": {auth.PermFlowsView}, "flows-edit": {auth.PermFlowsEdit}, "messages-view": {auth.PermMessagesView}}
	for name, perms := range users {
		if err := provider.CreateUser(context.Background(), auth.User{Username: name, PasswordHash: "pw", Permissions: perms}); err != nil {
			t.Fatal(err)
		}
	}
	flows := flowAdapter{store: state.NewMemStore()}
	if err := flows.Create(context.Background(), gateway.Flow{ID: "admit", Name: "Patient Admit"}); err != nil {
		t.Fatal(err)
	}
	gw := gateway.New(gateway.Config{
		Auth: authAdapter{provider}, Authorizer: authorizerAdapter{}, Passwords: provider,
		Flows: flows, Messages: &messageAdapter{store: state.NewMemStore()}, Topology: topologyAdapter{flows: flows}, RequireCSRF: true,
	})
	ts := httptest.NewServer(gw.Router())
	defer ts.Close()
	c := apiClient{t: t, base: ts.URL}

	for _, rt := range protectedRoutes {
		for name := range users {
			t.Run(rt.method+" "+rt.path+" as "+name, func(t *testing.T) {
				status, body, _ := c.do(rt.method, rt.path, `{"id":"x"}`, basic(name, "pw"))
				allowed := rt.perm == "" || strings.ReplaceAll(rt.perm, ":", "-") == name
				if allowed && status == http.StatusForbidden {
					t.Errorf("got 403 %q, want allowed", body)
				}
				if !allowed && (status != http.StatusForbidden || !strings.Contains(body, "missing permission "+rt.perm)) {
					t.Errorf("got %d %q, want 403 missing %s", status, body, rt.perm)
				}
			})
		}
	}
}

// TestBootstrapGeneratedPassword proves D-22: a one-time password is printed
// once and must be changed before the API is usable.
func TestBootstrapGeneratedPassword(t *testing.T) {
	t.Setenv(envBootstrapPassword, "")
	var out bytes.Buffer
	c := startComposed(t, serverconfig.Default(), &out)

	m := regexp.MustCompile(`password: (\S+)`).FindStringSubmatch(out.String())
	if m == nil {
		t.Fatalf("no generated password printed: %q", out.String())
	}
	generated := m[1]

	status, token := c.login(bootstrapAdmin, generated)
	if status != http.StatusOK {
		t.Fatalf("login with generated password: %d", status)
	}
	if status, body, _ := c.do(http.MethodGet, "/api/v1/flows", "", bearer(token)); status != http.StatusForbidden ||
		!strings.Contains(body, "PASSWORD_CHANGE_REQUIRED") {
		t.Errorf("before change: %d %q, want 403 PASSWORD_CHANGE_REQUIRED", status, body)
	}
	if status, body, _ := c.do(http.MethodGet, "/api/v1/auth/me", "", bearer(token)); status != http.StatusOK ||
		!strings.Contains(body, `"mustChangePassword":true`) {
		t.Errorf("me before change: %d %q", status, body)
	}
	if status, body, _ := c.do(http.MethodPost, "/api/v1/auth/password", `{"oldPassword":"`+generated+`","newPassword":"short"}`, bearer(token)); status != http.StatusBadRequest ||
		!strings.Contains(body, "PASSWORD_REJECTED") {
		t.Errorf("weak new password: %d %q, want 400 PASSWORD_REJECTED", status, body)
	}
	if status, _, _ := c.do(http.MethodPost, "/api/v1/auth/password", "not json", bearer(token)); status != http.StatusBadRequest {
		t.Errorf("malformed change: %d, want 400", status)
	}
	if status, body, _ := c.do(http.MethodPost, "/api/v1/auth/password", `{"oldPassword":"`+generated+`","newPassword":"`+generated+`"}`, bearer(token)); status != http.StatusBadRequest ||
		!strings.Contains(body, "PASSWORD_REJECTED") {
		t.Errorf("unchanged password: %d %q, want 400 PASSWORD_REJECTED", status, body)
	}
	if status, body, _ := c.do(http.MethodPost, "/api/v1/auth/password", `{"oldPassword":"wrong","newPassword":"New-Admin-Pass-2"}`, bearer(token)); status != http.StatusBadRequest ||
		!strings.Contains(body, "OLD_PASSWORD_INCORRECT") {
		t.Errorf("wrong old password: %d %q, want 400 OLD_PASSWORD_INCORRECT", status, body)
	}
	_, other := c.login(bootstrapAdmin, generated)
	if status, _, _ := c.do(http.MethodPost, "/api/v1/auth/password", `{"oldPassword":"`+generated+`","newPassword":"New-Admin-Pass-2"}`, bearer(token)); status != http.StatusNoContent {
		t.Fatalf("change password: %d, want 204", status)
	}
	if status, _, _ := c.do(http.MethodGet, "/api/v1/flows", "", bearer(token)); status != http.StatusOK {
		t.Errorf("token after change: %d, want 200", status)
	}
	if status, _, _ := c.do(http.MethodGet, "/api/v1/auth/me", "", bearer(other)); status != http.StatusUnauthorized {
		t.Errorf("other session after change: %d, want 401 (revoked)", status)
	}
	if status, _, _ := c.do(http.MethodGet, "/api/v1/flows", "", basic(bootstrapAdmin, "New-Admin-Pass-2")); status != http.StatusOK {
		t.Errorf("new password via Basic: %d, want 200", status)
	}
	if status, _, _ := c.do(http.MethodGet, "/api/v1/flows", "", basic(bootstrapAdmin, generated)); status != http.StatusUnauthorized {
		t.Errorf("old password: %d, want 401", status)
	}
}

// TestBootstrapPasswordSources proves the env and secret-file sources, and
// that config-level auth policy (auth.passwordPolicy, auth.lockout) applies.
func TestBootstrapPasswordSources(t *testing.T) {
	t.Run("secret-file", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "admin-password")
		if err := os.WriteFile(file, []byte("File-Admin-Pass-3\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv(envBootstrapPassword, "")
		t.Setenv(envBootstrapPasswordFile, file)
		var out bytes.Buffer
		c := startComposed(t, serverconfig.Default(), &out)
		if out.Len() != 0 {
			t.Errorf("printed output for a provided password: %q", out.String())
		}
		if status, _, _ := c.do(http.MethodGet, "/api/v1/flows", "", basic(bootstrapAdmin, "File-Admin-Pass-3")); status != http.StatusOK {
			t.Errorf("file password: %d, want 200", status)
		}
	})

	t.Run("lockout-policy", func(t *testing.T) {
		cfg := serverconfig.Default()
		cfg.Auth.Lockout = serverconfig.Lockout{RetryLimit: 2, LockoutPeriodSeconds: 600}
		c := startComposed(t, cfg, io.Discard)
		for i := 0; i < 2; i++ {
			c.do(http.MethodGet, "/api/v1/system", "", basic(bootstrapAdmin, "wrong"))
		}
		if status, _, _ := c.do(http.MethodGet, "/api/v1/system", "", basic(bootstrapAdmin, testAdminPassword)); status != http.StatusUnauthorized {
			t.Errorf("correct password while locked: %d, want 401", status)
		}
	})

	errCases := []struct {
		name, yaml, env, file, want string
	}{
		{name: "policy-rejects-env-password", yaml: "auth: {passwordPolicy: {minLength: 40}}\n", env: testAdminPassword, want: "admin password rejected by auth.passwordPolicy"},
		{name: "unreadable-secret-file", env: "", file: "/nonexistent/admin-password", want: "Error: bootstrap:"},
	}
	for _, tt := range errCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(envBootstrapPassword, tt.env)
			t.Setenv(envBootstrapPasswordFile, tt.file)
			var errb bytes.Buffer
			if code := run([]string{"server", "--config", writeConfig(t, tt.yaml), freeAddr(t)}, strings.NewReader(""), io.Discard, &errb); code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			if !strings.Contains(errb.String(), tt.want) {
				t.Errorf("stderr %q does not contain %q", errb.String(), tt.want)
			}
		})
	}
}

func TestGeneratePasswordSatisfiesPolicy(t *testing.T) {
	policies := []auth.PasswordPolicy{
		{MinLength: 8, MinUpper: 1, MinLower: 1, MinNumeric: 1},
		{MinLength: 40, MinUpper: 3, MinLower: 3, MinNumeric: 3, MinSpecial: 3},
		{MinLength: 12, MinUpper: -1, MinLower: 2, MinNumeric: -1, MinSpecial: -1},
	}
	for _, p := range policies {
		pw, err := generatePassword(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Validate(pw); err != nil {
			t.Errorf("policy %+v rejected generated %q: %v", p, pw, err)
		}
	}
}

// TestCLICredentials proves -u/-p are sent by batch mode.
func TestCLICredentials(t *testing.T) {
	c := startComposed(t, serverconfig.Default(), io.Discard)
	path := filepath.Join(t.TempDir(), "script.txt")
	if err := os.WriteFile(path, []byte("flow list\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		args   []string
		want   int
		stderr string
	}{
		{name: "with-credentials", args: []string{"-a", c.base, "-u", bootstrapAdmin, "-p", testAdminPassword, "-s", path}, want: 0},
		{name: "without-credentials", args: []string{"-a", c.base, "-s", path}, want: 2, stderr: "401 Unauthorized"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			if code := run(tt.args, strings.NewReader(""), &out, &errb); code != tt.want {
				t.Errorf("exit = %d, want %d (stderr %q)", code, tt.want, errb.String())
			}
			if !strings.Contains(errb.String(), tt.stderr) {
				t.Errorf("stderr %q does not contain %q", errb.String(), tt.stderr)
			}
		})
	}
}
