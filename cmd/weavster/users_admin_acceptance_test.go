package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestUserAdministration: an administrator manages accounts over the API
// and CLI; a created account gets exactly its permissions, and changes end
// its sessions.
func TestUserAdministration(t *testing.T) {
	c := startComposed(t, serverconfig.Default(), io.Discard)
	admin := basic(bootstrapAdmin, testAdminPassword)
	const pw = "Viewer-Passw0rd"
	token := func(user, pass string) string {
		t.Helper()
		code, body, _ := c.do(http.MethodPost, "/api/v1/auth/login", `{"username":"`+user+`","password":"`+pass+`"}`, nil)
		var r struct{ Token string }
		_ = json.Unmarshal([]byte(body), &r)
		if code != http.StatusOK || r.Token == "" {
			t.Fatalf("login %s: %d %q", user, code, body)
		}
		return r.Token
	}
	steps := []struct {
		name, method, path, body string
		creds                    func(*http.Request)
		status                   int
		want                     string
	}{
		{"create", http.MethodPost, "/api/v1/users", `{"username":"viewer","password":"` + pw + `","permissions":["flows:view"],"mustChangePassword":false}`, admin, http.StatusCreated, `"permissions":["flows:view"]`},
		{"duplicate", http.MethodPost, "/api/v1/users", `{"username":"viewer","password":"` + pw + `","permissions":[]}`, admin, http.StatusConflict, "user already exists"},
		{"bad username", http.MethodPost, "/api/v1/users", `{"username":"a:b","password":"` + pw + `","permissions":[]}`, admin, http.StatusBadRequest, "username must be"},
		{"unknown permission", http.MethodPost, "/api/v1/users", `{"username":"x","password":"` + pw + `","permissions":["flows:fly"]}`, admin, http.StatusBadRequest, `unknown permission \"flows:fly\"`},
		{"weak password", http.MethodPost, "/api/v1/users", `{"username":"x","password":"a","permissions":[]}`, admin, http.StatusBadRequest, "invalid user"},
		{"dots only", http.MethodPost, "/api/v1/users", `{"username":"..","password":"` + pw + `","permissions":[]}`, admin, http.StatusBadRequest, "username must be"},
		{"unknown field", http.MethodPost, "/api/v1/users", `{"username":"x","colour":"red"}`, admin, http.StatusBadRequest, "unknown field"},
		{"list", http.MethodGet, "/api/v1/users", ``, admin, http.StatusOK, `"username":"viewer"`},
		{"no password data", http.MethodGet, "/api/v1/users/viewer", ``, admin, http.StatusOK, `"locked":false`},
		{"viewer may read flows", http.MethodGet, "/api/v1/flows", ``, basic("viewer", pw), http.StatusOK, "["},
		{"viewer may not edit flows", http.MethodPost, "/api/v1/flows", `{"id":"x"}`, basic("viewer", pw), http.StatusForbidden, "missing permission flows:edit"},
		{"viewer may not administer users", http.MethodGet, "/api/v1/users", ``, basic("viewer", pw), http.StatusForbidden, "missing permission users:admin"},
		{"cannot delete yourself", http.MethodDelete, "/api/v1/users/admin", ``, admin, http.StatusConflict, "your own account"},
		{"last admin keeps admin", http.MethodPut, "/api/v1/users/admin", `{"permissions":["flows:view"]}`, admin, http.StatusConflict, "last account with the admin permission"},
		{"unknown user", http.MethodGet, "/api/v1/users/nobody", ``, admin, http.StatusNotFound, "user not found"},
		{"password for an unknown user", http.MethodPost, "/api/v1/users/nobody/password", `{"password":"a"}`, admin, http.StatusNotFound, "user not found"},
	}
	for _, s := range steps {
		code, body, _ := c.do(s.method, s.path, s.body, s.creds)
		if code != s.status || !strings.Contains(body, s.want) {
			t.Errorf("%s: %d %q; want %d containing %q", s.name, code, body, s.status, s.want)
		}
	}

	// Changing permissions ends the viewer's sessions; the new ones apply.
	tok := token("viewer", pw)
	if code, _, _ := c.do(http.MethodPut, "/api/v1/users/viewer", `{"permissions":["flows:view","flows:edit"]}`, admin); code != http.StatusOK {
		t.Fatalf("update: %d", code)
	}
	if code, _, _ := c.do(http.MethodGet, "/api/v1/flows", "", bearer(tok)); code != http.StatusUnauthorized {
		t.Errorf("old session after a permission change: %d, want 401", code)
	}
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"made-by-viewer"}`, basic("viewer", pw)); code != http.StatusCreated {
		t.Errorf("viewer with flows:edit: %d %q", code, body)
	}
	// A reset password must be changed before anything else.
	tok = token("viewer", pw)
	if code, _, _ := c.do(http.MethodPost, "/api/v1/users/viewer/password", `{"password":"Reset-Passw0rd"}`, admin); code != http.StatusNoContent {
		t.Fatalf("reset: %d", code)
	}
	if code, _, _ := c.do(http.MethodGet, "/api/v1/flows", "", bearer(tok)); code != http.StatusUnauthorized {
		t.Errorf("old session after a reset: %d, want 401", code)
	}
	if code, body, _ := c.do(http.MethodGet, "/api/v1/flows", "", basic("viewer", "Reset-Passw0rd")); code != http.StatusForbidden || !strings.Contains(body, "PASSWORD_CHANGE_REQUIRED") {
		t.Errorf("after a reset: %d %q, want 403 PASSWORD_CHANGE_REQUIRED", code, body)
	}
	if code, _, _ := c.do(http.MethodDelete, "/api/v1/users/viewer", "", admin); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if code, _, _ := c.do(http.MethodGet, "/api/v1/flows", "", basic("viewer", "Reset-Passw0rd")); code != http.StatusUnauthorized {
		t.Errorf("deleted user: %d, want 401", code)
	}

	// A users:admin account without admin cannot escalate.
	const upw = "UserAdm-Passw0rd"
	c.do(http.MethodPost, "/api/v1/users", `{"username":"useradm","password":"`+upw+`","permissions":["users:admin","flows:view"],"mustChangePassword":false}`, admin)
	ua := basic("useradm", upw)
	for _, s := range []struct {
		name, method, path, body string
		status                   int
		want                     string
	}{
		{"grant admin", http.MethodPost, "/api/v1/users", `{"username":"x1","password":"` + pw + `","permissions":["admin"]}`, http.StatusForbidden, "only admin can grant admin"},
		{"grant a permission not held", http.MethodPost, "/api/v1/users", `{"username":"x2","password":"` + pw + `","permissions":["flows:edit"]}`, http.StatusForbidden, "cannot grant flows:edit"},
		{"make itself admin", http.MethodPut, "/api/v1/users/useradm", `{"permissions":["admin"]}`, http.StatusForbidden, "cannot grant admin"},
		{"reset the admin's password", http.MethodPost, "/api/v1/users/admin/password", `{"password":"Hijack-Passw0rd"}`, http.StatusForbidden, "only an account with admin"},
		{"delete the admin", http.MethodDelete, "/api/v1/users/admin", ``, http.StatusForbidden, "only an account with admin"},
		{"grant what it holds", http.MethodPost, "/api/v1/users", `{"username":"x3","password":"` + pw + `","permissions":["flows:view"]}`, http.StatusCreated, `"username":"x3"`},
	} {
		code, body, _ := c.do(s.method, s.path, s.body, ua)
		if code != s.status || !strings.Contains(body, s.want) {
			t.Errorf("useradm %s: %d %q; want %d containing %q", s.name, code, body, s.status, s.want)
		}
	}
	// Editing your own account keeps your own session.
	own := token("useradm", upw)
	if code, body, _ := c.do(http.MethodPut, "/api/v1/users/useradm", `{"permissions":["users:admin","flows:view"],"email":"me@example.com","org":"Radiology"}`, bearer(own)); code != http.StatusOK || !strings.Contains(body, `"email":"me@example.com","org":"Radiology"`) {
		t.Errorf("own update: %d %q", code, body)
	}
	if code, _, _ := c.do(http.MethodGet, "/api/v1/users", "", bearer(own)); code != http.StatusOK {
		t.Errorf("own session after updating yourself: %d, want 200", code)
	}
	// Setting your own password ends every session, your own too.
	if code, _, _ := c.do(http.MethodPost, "/api/v1/users/useradm/password", `{"password":"Self-Reset-Passw0rd"}`, bearer(own)); code != http.StatusNoContent {
		t.Fatalf("self reset: %d", code)
	}
	if code, _, _ := c.do(http.MethodGet, "/api/v1/users", "", bearer(own)); code != http.StatusUnauthorized {
		t.Errorf("own session after resetting your own password: %d, want 401", code)
	}
	// Omitted email and org keep them.
	if _, body, _ := c.do(http.MethodPut, "/api/v1/users/useradm", `{"permissions":["users:admin","flows:view"]}`, admin); !strings.Contains(body, `"email":"me@example.com","org":"Radiology"`) {
		t.Errorf("update without email and org cleared them: %s", body)
	}

	// CLI.
	script := filepath.Join(t.TempDir(), "s.txt")
	for _, tt := range []struct {
		line, want string
		code       int
	}{
		{"user add ops Ops-Passw0rd flows:view events:view", "added ops", 0},
		{"user list", "ops\tevents:view,flows:view\tmust change password", 0},
		{"user changepw ops Other-Passw0rd", "password set for ops", 0},
		{"user remove ops", "removed ops", 0},
		{"user remove ops", "", 2},
		{"user add", "", 2},
	} {
		_ = os.WriteFile(script, []byte(tt.line+"\n"), 0o600)
		var out, errb bytes.Buffer
		if code := run([]string{"-a", c.base, "-u", bootstrapAdmin, "-p", testAdminPassword, "-s", script}, strings.NewReader(""), &out, &errb); code != tt.code || !strings.Contains(out.String(), tt.want) {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", tt.line, code, out.String(), errb.String())
		}
	}
}
