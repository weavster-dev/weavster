package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestGitIntegrationRemoved: the in-server Git integration is not part of
// the MVP (D-55): no git.* server config, no /api/v1/git routes, no gitRev
// on config plan, and no git permissions.
func TestGitIntegrationRemoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "weavster.yaml")
	if err := os.WriteFile(path, []byte("git: {path: /var/lib/weavster/repo}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := serverconfig.Load(path); err == nil || !strings.Contains(err.Error(), "field git not found") {
		t.Errorf("git server config = %v", err)
	}

	c := startComposed(t, serverconfig.Default(), io.Discard)
	admin := basic(bootstrapAdmin, testAdminPassword)
	// Every route the integration had, on the versioned path and the
	// unversioned alias.
	for _, prefix := range []string{"/api/v1", "/api"} {
		for _, rt := range []string{
			"GET /git", "GET /git/log", "GET /git/content", "GET /git/diff", "GET /git/drift", "GET /git/remote",
			"POST /git/commit", "POST /git/restore", "POST /git/push", "POST /git/pull",
		} {
			method, path, _ := strings.Cut(rt, " ")
			if code, body, _ := c.do(method, prefix+path, "", admin); code != http.StatusNotFound || !strings.Contains(body, gateway.RouteNotFoundMessage) {
				t.Errorf("%s %s%s: %d %s", method, prefix, path, code, body)
			}
		}
	}
	// gitRev is not a document source: the plan reads the (empty) body.
	if code, _, _ := c.do(http.MethodPost, "/api/v1/config/plan?gitRev=HEAD", "", admin); code != http.StatusBadRequest {
		t.Errorf("plan with gitRev: %d", code)
	}
	if code, body, _ := c.do(http.MethodPost, "/api/v1/users", `{"username":"g","password":"Git-Passw0rd-1","permissions":["git:view"],"mustChangePassword":false}`, admin); code != http.StatusBadRequest || !strings.Contains(body, "git:view") {
		t.Errorf("git permission: %d %s", code, body)
	}
}
