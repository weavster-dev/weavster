package main

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// A self-service permission reduction must revoke the identity cached in
// the caller's token, just as a permission change by another admin does.
func TestSelfPermissionReductionRevokesSessions(t *testing.T) {
	c := startComposed(t, serverconfig.Default(), io.Discard)
	const password = "Self-Perm-Passw0rd"
	code, _, _ := c.do(http.MethodPost, "/api/v1/users",
		`{"username":"self","password":"`+password+`","permissions":["users:admin","flows:view"],"mustChangePassword":false}`,
		basic(bootstrapAdmin, testAdminPassword))
	if code != http.StatusCreated {
		t.Fatalf("create: status %d", code)
	}
	login := func() string {
		t.Helper()
		code, body, _ := c.do(http.MethodPost, "/api/v1/auth/login",
			`{"username":"self","password":"`+password+`"}`, nil)
		var result struct{ Token string }
		if err := json.Unmarshal([]byte(body), &result); err != nil || code != http.StatusOK || result.Token == "" {
			t.Fatalf("login: status %d, decode error %v", code, err)
		}
		return result.Token
	}
	own, other := login(), login()
	if code, _, _ := c.do(http.MethodPut, "/api/v1/users/self",
		`{"permissions":["users:admin"]}`, bearer(own)); code != http.StatusOK {
		t.Fatalf("self permission change: status %d", code)
	}
	for _, token := range []string{own, other} {
		if code, _, _ := c.do(http.MethodGet, "/api/v1/flows", "", bearer(token)); code != http.StatusUnauthorized {
			t.Errorf("old session after self permission reduction: status %d, want 401", code)
		}
	}
	// The remaining permission works after reauthentication, while the
	// removed permission cannot be exercised or granted back.
	fresh := bearer(login())
	for _, tc := range []struct {
		method, path, body string
		want               int
	}{
		{http.MethodGet, "/api/v1/users", "", http.StatusOK},
		{http.MethodGet, "/api/v1/flows", "", http.StatusForbidden},
		{http.MethodPut, "/api/v1/users/self", `{"permissions":["users:admin","flows:view"]}`, http.StatusForbidden},
	} {
		if code, _, _ := c.do(tc.method, tc.path, tc.body, fresh); code != tc.want {
			t.Errorf("%s %s after reauthentication: status %d, want %d", tc.method, tc.path, code, tc.want)
		}
	}
}
