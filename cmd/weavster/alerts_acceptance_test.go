package main

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAlerts: alert definitions are created, validated, enabled/disabled,
// imported (with and without force), exported by id, name, or all, are
// permission-checked, and survive a restart (on PostgreSQL).
func TestAlerts(t *testing.T) {
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\n"+storeConfig(t))
	args := []string{"server", "--config", cfg}
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	stop := startCLI(t, args, c.base+"/api/openapi.yaml")

	errorsAlert := `{"id":"errors","name":"ADT errors","enabled":true,"trigger":{"events":["message.errored"],"flows":["adt"]},"actions":[{"type":"email","to":["ops@example.com"]}]}`
	steps := []struct {
		name, method, path, body string
		status                   int
		want                     string
	}{
		{"create", http.MethodPost, "/api/v1/alerts", errorsAlert, http.StatusCreated, `"id":"errors"`},
		{"duplicate", http.MethodPost, "/api/v1/alerts", errorsAlert, http.StatusConflict, "already exists"},
		{"invalid", http.MethodPost, "/api/v1/alerts", `{"id":"x","name":"X","trigger":{"events":["message.sent"]},"actions":[{"type":"webhook","url":"https://h/x"}]}`, http.StatusBadRequest, "unknown trigger event"},
		{"unknown field", http.MethodPost, "/api/v1/alerts", `{"id":"x","nmae":"X"}`, http.StatusBadRequest, "unknown field"},
		{"disable", http.MethodPost, "/api/v1/alerts/errors/disable", ``, http.StatusOK, `"enabled":false`},
		{"options", http.MethodGet, "/api/v1/alerts/options", ``, http.StatusOK, `"message.dead-lettered"`},
		{"unversioned", http.MethodGet, "/api/alerts/errors", ``, http.StatusOK, `"name":"ADT errors"`},
	}
	for _, s := range steps {
		code, body, _ := c.do(s.method, s.path, s.body, admin)
		if code != s.status || !strings.Contains(body, s.want) {
			t.Errorf("%s: %d %q; want %d containing %q", s.name, code, body, s.status, s.want)
		}
	}
	c.do(http.MethodPost, "/api/v1/users", `{"username":"viewer","password":"Viewer-Passw0rd","permissions":["flows:view"],"mustChangePassword":false}`, admin)
	if code, body, _ := c.do(http.MethodGet, "/api/v1/alerts", "", basic("viewer", "Viewer-Passw0rd")); code != http.StatusForbidden || !strings.Contains(body, "alerts:edit") {
		t.Errorf("viewer: %d %q", code, body)
	}
	stop()
	if !restartable(t) {
		return
	}

	stop = startCLI(t, args, c.base+"/api/openapi.yaml")
	defer stop()
	if _, body, _ := c.do(http.MethodGet, "/api/v1/alerts/errors", "", admin); !strings.Contains(body, `"enabled":false`) {
		t.Errorf("after restart = %s", body)
	}

	// CLI: export by name, by id, and all; import without and with force.
	dir := t.TempDir()
	one, all, script := filepath.Join(dir, "one.json"), filepath.Join(dir, "all.json"), filepath.Join(dir, "s.txt")
	c.do(http.MethodPost, "/api/v1/alerts", strings.Replace(strings.Replace(errorsAlert, `"id":"errors"`, `"id":"dead"`, 1), `"ADT errors"`, `"Dead letters"`, 1), admin)
	for _, tt := range []struct {
		line, want string
		code       int
	}{
		{`exportalert "ADT errors" "` + one + `"`, "exported 1 alerts to " + one, 0},
		{`exportalert * "` + all + `"`, "exported 2 alerts", 0},
		{`exportalert dead "` + filepath.Join(dir, "dead.json") + `"`, "exported 1 alerts", 0},
		{`importalert "` + all + `"`, "", 2},
		{`importalert "` + all + `" force`, "imported 2 alerts from " + all, 0},
		{`exportalert nope "` + one + `"`, "", 2},
		{`importalert "` + all + `" overwrite`, "", 2},
		{`exportalert "` + one + `"`, "", 2},
	} {
		_ = os.WriteFile(script, []byte(tt.line+"\n"), 0o600)
		var out, errb bytes.Buffer
		if code := run([]string{"-a", c.base, "-u", bootstrapAdmin, "-p", testAdminPassword, "-s", script}, strings.NewReader(""), &out, &errb); code != tt.code || !strings.Contains(out.String(), tt.want) {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", tt.line, code, out.String(), errb.String())
		}
	}
	// Names are not unique: an id match wins, and a shared name is refused.
	c.do(http.MethodPost, "/api/v1/alerts", strings.Replace(errorsAlert, `"id":"errors"`, `"id":"e2"`, 1), admin)
	for _, tt := range []struct {
		line, want string
		code       int
	}{
		{`exportalert errors "` + filepath.Join(dir, "id.json") + `"`, "exported 1 alerts", 0},
		{`exportalert "ADT errors" "` + filepath.Join(dir, "name.json") + `"`, "", 2},
	} {
		_ = os.WriteFile(script, []byte(tt.line+"\n"), 0o600)
		var out, errb bytes.Buffer
		if code := run([]string{"-a", c.base, "-u", bootstrapAdmin, "-p", testAdminPassword, "-s", script}, strings.NewReader(""), &out, &errb); code != tt.code || !strings.Contains(out.String(), tt.want) {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", tt.line, code, out.String(), errb.String())
		}
	}
	if b, _ := os.ReadFile(one); !strings.HasPrefix(strings.TrimSpace(string(b)), "[") || !strings.Contains(string(b), `"id": "errors"`) {
		t.Errorf("exported file = %s", b)
	}
}
