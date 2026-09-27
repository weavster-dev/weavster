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
	for _, tt := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/git"},
		{http.MethodPost, "/api/v1/git/commit"},
		{http.MethodGet, "/api/v1/git/drift"},
		{http.MethodPost, "/api/v1/git/pull"},
	} {
		if code, body, _ := c.do(tt.method, tt.path, "", admin); code != http.StatusNotFound || !strings.Contains(body, gateway.RouteNotFoundMessage) {
			t.Errorf("%s %s: %d %s", tt.method, tt.path, code, body)
		}
	}
	// gitRev is not a document source: the plan reads the (empty) body.
	if code, body, _ := c.do(http.MethodPost, "/api/v1/config/plan?gitRev=HEAD", "", admin); code != http.StatusBadRequest || strings.Contains(body, "git") {
		t.Errorf("plan with gitRev: %d %s", code, body)
	}
	if code, body, _ := c.do(http.MethodPost, "/api/v1/users", `{"username":"g","password":"Git-Passw0rd-1","permissions":["git:view"],"mustChangePassword":false}`, admin); code != http.StatusBadRequest || !strings.Contains(body, "git:view") {
		t.Errorf("git permission: %d %s", code, body)
	}
}
