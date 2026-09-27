package main

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSnippets: code snippets and snippet libraries are managed over the API
// and CLI, keep library references valid, are permission-checked, and
// survive a restart with the SQLite store.
func TestSnippets(t *testing.T) {
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n")
	args := []string{"server", "--config", cfg}
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	stop := startCLI(t, args, c.base+"/api/openapi.yaml")

	steps := []struct {
		name, method, path, body string
		status                   int
		want                     string
	}{
		{"create library", http.MethodPost, "/api/v1/snippet-libraries", `{"name":"hl7","description":"HL7 helpers"}`, http.StatusCreated, `"name":"hl7"`},
		{"create snippet", http.MethodPost, "/api/v1/snippets", `{"name":"pid","library":"hl7","code":"get('PID')"}`, http.StatusCreated, `"library":"hl7"`},
		{"duplicate", http.MethodPost, "/api/v1/snippets", `{"name":"pid"}`, http.StatusConflict, "already exists"},
		{"unknown library", http.MethodPut, "/api/v1/snippets/x", `{"library":"nope"}`, http.StatusNotFound, "snippet library not found"},
		{"bulk update", http.MethodPut, "/api/v1/snippets", `[{"name":"trim","code":"trim()"}]`, http.StatusOK, `"name":"trim"`},
		{"summary", http.MethodGet, "/api/v1/snippets?summary=true", ``, http.StatusOK, `[{"name":"pid","library":"hl7"},{"name":"trim"}]`},
		{"library in use", http.MethodDelete, "/api/v1/snippet-libraries/hl7", ``, http.StatusConflict, "still has snippets"},
		{"unversioned", http.MethodGet, "/api/snippets/pid", ``, http.StatusOK, `"code":"get('PID')"`},
	}
	for _, s := range steps {
		code, body, _ := c.do(s.method, s.path, s.body, admin)
		if code != s.status || !strings.Contains(body, s.want) {
			t.Errorf("%s: %d %q; want %d containing %q", s.name, code, body, s.status, s.want)
		}
	}
	c.do(http.MethodPost, "/api/v1/users", `{"username":"viewer","password":"Viewer-Passw0rd","permissions":["flows:view"],"mustChangePassword":false}`, admin)
	if code, body, _ := c.do(http.MethodGet, "/api/v1/snippets", "", basic("viewer", "Viewer-Passw0rd")); code != http.StatusForbidden || !strings.Contains(body, "snippets:edit") {
		t.Errorf("viewer: %d %q", code, body)
	}
	stop()

	stop = startCLI(t, args, c.base+"/api/openapi.yaml")
	defer stop()
	if _, body, _ := c.do(http.MethodGet, "/api/v1/snippets/pid", "", admin); !strings.Contains(body, `"code":"get('PID')"`) {
		t.Errorf("after restart = %s", body)
	}

	// CLI: export, remove, import back, list.
	dir := t.TempDir()
	snippetsFile, libsFile := filepath.Join(dir, "snippets.json"), filepath.Join(dir, "libs.json")
	script := filepath.Join(dir, "s.txt")
	for _, tt := range []struct {
		line, want string
		code       int
	}{
		{`snippet library export "` + libsFile + `"`, "exported 1 snippet libraries to " + libsFile, 0},
		{`snippet export "` + snippetsFile + `"`, "exported 2 snippets", 0},
		{`snippet remove pid`, "removed pid", 0},
		{`snippet library remove hl7`, "removed hl7", 0},
		{`snippet library import "` + libsFile + `"`, "imported 1 snippet libraries", 0},
		{`snippet import "` + snippetsFile + `"`, "imported 2 snippets", 0},
		{`snippet list`, "pid\thl7\ntrim\n", 0},
		{`snippet library list`, "hl7\tHL7 helpers\n", 0},
		{`snippet remove pid extra`, "", 2},
		{`snippet library`, "", 2},
		{`snippet rename a`, "", 2},
		{`snippet remove nope`, "", 2},
		{`snippet import "` + filepath.Join(dir, "none.json") + `"`, "", 2},
	} {
		_ = os.WriteFile(script, []byte(tt.line+"\n"), 0o600)
		var out, errb bytes.Buffer
		if code := run([]string{"-a", c.base, "-u", bootstrapAdmin, "-p", testAdminPassword, "-s", script}, strings.NewReader(""), &out, &errb); code != tt.code || !strings.Contains(out.String(), tt.want) {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", tt.line, code, out.String(), errb.String())
		}
	}
}
