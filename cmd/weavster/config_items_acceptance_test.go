package main

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigItems: the config map, global scripts, and settings are managed
// over the API and CLI, validated, permission-checked, and survive a
// restart (on PostgreSQL).
func TestConfigItems(t *testing.T) {
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\n"+storeConfig(t))
	args := []string{"server", "--config", cfg}
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	stop := startCLI(t, args, c.base+"/api/openapi.yaml")

	steps := []struct {
		name, method, path, body string
		creds                    func(*http.Request)
		status                   int
		want                     string
	}{
		{"replace map", http.MethodPut, "/api/v1/configmap", `{"region":"eu","db.host":"db1"}`, admin, http.StatusOK, `"region":"eu"`},
		{"one entry", http.MethodPut, "/api/v1/configmap/region", `{"value":"us"}`, admin, http.StatusOK, `"value":"us"`},
		{"map needs strings", http.MethodPut, "/api/v1/configmap/port", `{"value":8080}`, admin, http.StatusBadRequest, "must be a string"},
		{"script", http.MethodPut, "/api/v1/scripts/deploy", `{"value":"log('deployed')"}`, admin, http.StatusOK, `"name":"deploy"`},
		{"setting", http.MethodPut, "/api/v1/settings/retention", `{"value":{"days":30}}`, admin, http.StatusOK, `"days":30`},
		{"delete", http.MethodDelete, "/api/v1/configmap/db.host", ``, admin, http.StatusNoContent, ""},
		{"unknown", http.MethodGet, "/api/v1/configmap/db.host", ``, admin, http.StatusNotFound, "config map entry not found"},
		{"unversioned", http.MethodGet, "/api/settings", ``, admin, http.StatusOK, `"retention"`},
	}
	for _, s := range steps {
		code, body, _ := c.do(s.method, s.path, s.body, s.creds)
		if code != s.status || !strings.Contains(body, s.want) {
			t.Errorf("%s: %d %q; want %d containing %q", s.name, code, body, s.status, s.want)
		}
	}
	// A user without configmap:edit is refused.
	c.do(http.MethodPost, "/api/v1/users", `{"username":"viewer","password":"Viewer-Passw0rd","permissions":["flows:view"],"mustChangePassword":false}`, admin)
	if code, body, _ := c.do(http.MethodGet, "/api/v1/configmap", "", basic("viewer", "Viewer-Passw0rd")); code != http.StatusForbidden || !strings.Contains(body, "configmap:edit") {
		t.Errorf("viewer: %d %q", code, body)
	}
	stop()
	if !restartable(t) {
		return
	}

	stop = startCLI(t, args, c.base+"/api/openapi.yaml")
	defer stop()
	for path, want := range map[string]string{
		"/api/v1/configmap":          `{"region":"us"}`,
		"/api/v1/scripts/deploy":     `"value":"log('deployed')"`,
		"/api/v1/settings/retention": `"days":30`,
	} {
		if _, body, _ := c.do(http.MethodGet, path, "", admin); !strings.Contains(body, want) {
			t.Errorf("after restart %s = %s, want %s", path, body, want)
		}
	}

	// CLI: export the map, change it, import the file back.
	dir := t.TempDir()
	mapFile, scriptsFile := filepath.Join(dir, "map.json"), filepath.Join(dir, "scripts.json")
	script := filepath.Join(dir, "s.txt")
	for _, tt := range []struct {
		line, want string
		code       int
	}{
		{`exportmap "` + mapFile + `"`, "exported 1 config map entries to " + mapFile, 0},
		{`exportscripts "` + scriptsFile + `"`, "exported 1 scripts", 0},
		{`importmap "` + mapFile + `"`, "imported 1 config map entries", 0},
		{`importscripts "` + filepath.Join(dir, "none.json") + `"`, "", 2},
		{`importmap`, "", 2},
	} {
		if tt.line == `importmap "`+mapFile+`"` {
			c.do(http.MethodPut, "/api/v1/configmap", `{"changed":"x"}`, admin)
		}
		_ = os.WriteFile(script, []byte(tt.line+"\n"), 0o600)
		var out, errb bytes.Buffer
		if code := run([]string{"-a", c.base, "-u", bootstrapAdmin, "-p", testAdminPassword, "-s", script}, strings.NewReader(""), &out, &errb); code != tt.code || !strings.Contains(out.String(), tt.want) {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", tt.line, code, out.String(), errb.String())
		}
	}
	if _, body, _ := c.do(http.MethodGet, "/api/v1/configmap", "", admin); strings.TrimSpace(body) != `{"region":"us"}` {
		t.Errorf("after importmap = %s, want the exported map back", body)
	}
}
