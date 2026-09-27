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

// TestConfigTransfer: the full configuration is exported from one server
// and imported into others, with conflict detection and force, the config
// map kept or overwritten, deployment on or off, permission checks, and the
// CLI exportcfg/importcfg round trip including a missing file.
func TestConfigTransfer(t *testing.T) {
	admin := basic(bootstrapAdmin, testAdminPassword)
	dir := t.TempDir()
	src := startComposed(t, serverconfig.Default(), io.Discard)
	createFlow(t, src, `{"id":"a","enabled":true,"destinations":[{"name":"out","type":"file","dir":"`+dir+`"}]}`)
	src.do(http.MethodPost, "/api/v1/flows", `{"id":"b","enabled":false,"destinations":[{"name":"out","type":"file","dir":"`+dir+`"}]}`, admin)
	for _, s := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/snippet-libraries", `{"name":"hl7"}`},
		{http.MethodPost, "/api/v1/snippets", `{"name":"pid","library":"hl7","code":"x"}`},
		{http.MethodPost, "/api/v1/alerts", `{"id":"errors","name":"Errors","trigger":{"events":["message.errored"]},"actions":[{"type":"email","to":["ops@example.com"]}]}`},
		{http.MethodPut, "/api/v1/scripts/deploy", `{"value":"log()"}`},
		{http.MethodPut, "/api/v1/settings/retention", `{"value":{"days":30}}`},
		{http.MethodPut, "/api/v1/configmap/region", `{"value":"eu"}`},
	} {
		if code, body, _ := src.do(s.method, s.path, s.body, admin); code >= 300 {
			t.Fatalf("seed %s: %d %s", s.path, code, body)
		}
	}
	// Without includeConfigMap the export leaves the config map out.
	if _, body, _ := src.do(http.MethodGet, "/api/v1/config/export", "", admin); strings.Contains(body, `"configmap"`) || !strings.Contains(body, `"format":"weavster-config-v1"`) {
		t.Errorf("export = %s", body)
	}
	_, full, _ := src.do(http.MethodGet, "/api/v1/config/export?includeConfigMap=true", "", admin)

	dst := startComposed(t, serverconfig.Default(), io.Discard)
	dst.do(http.MethodPut, "/api/v1/configmap/region", `{"value":"us"}`, admin)
	status := func(id string) string {
		_, body, _ := dst.do(http.MethodGet, "/api/v1/flows/"+id, "", admin)
		var f struct{ Status string }
		_ = json.Unmarshal([]byte(body), &f)
		return f.Status
	}
	steps := []struct {
		name, path, body string
		code             int
		want, region     string
		statusA          string
	}{
		{"bad format", "/api/v1/config/import", `{"format":"x"}`, http.StatusBadRequest, "format must be", "us", ""},
		{"missing library", "/api/v1/config/import", `{"format":"weavster-config-v1","snippets":[{"name":"s","library":"nope"}]}`, http.StatusBadRequest, "nope", "us", ""},
		{"import, no deploy, map kept", "/api/v1/config/import?nodeploy=true", full, http.StatusOK, `"created":["a","b"]`, "us", "undeployed"},
		{"conflicts", "/api/v1/config/import", full, http.StatusConflict, "flow a, flow b, alert errors, snippet pid, library hl7", "us", "undeployed"},
		{"force, overwrite map, deploy", "/api/v1/config/import?force=true&overwriteConfigMap=true", full, http.StatusOK, `"deployed":["a"]`, "eu", "deployed"},
	}
	for _, s := range steps {
		code, body, _ := dst.do(http.MethodPost, s.path, s.body, admin)
		if code != s.code || !strings.Contains(body, s.want) {
			t.Errorf("%s: %d %q; want %d containing %q", s.name, code, body, s.code, s.want)
		}
		if _, m, _ := dst.do(http.MethodGet, "/api/v1/configmap/region", "", admin); !strings.Contains(m, `"`+s.region+`"`) {
			t.Errorf("%s: config map = %s, want region %s", s.name, m, s.region)
		}
		if s.statusA != "" && status("a") != s.statusA {
			t.Errorf("%s: flow a is %s, want %s", s.name, status("a"), s.statusA)
		}
	}
	if st := status("b"); st != "undeployed" {
		t.Errorf("disabled flow b = %s, want undeployed", st)
	}
	for path, want := range map[string]string{
		"/api/v1/snippets/pid": `"library":"hl7"`, "/api/v1/alerts/errors": `"name":"Errors"`,
		"/api/v1/scripts/deploy": `log()`, "/api/v1/settings/retention": `"days":30`,
	} {
		if _, body, _ := dst.do(http.MethodGet, path, "", admin); !strings.Contains(body, want) {
			t.Errorf("imported %s = %s", path, body)
		}
	}
	// A user without every configuration permission is refused.
	dst.do(http.MethodPost, "/api/v1/users", `{"username":"flows","password":"Flows-Passw0rd","permissions":["flows:view","flows:edit","flows:deploy"],"mustChangePassword":false}`, admin)
	if code, body, _ := dst.do(http.MethodGet, "/api/v1/config/export", "", basic("flows", "Flows-Passw0rd")); code != http.StatusForbidden || !strings.Contains(body, "missing permission") {
		t.Errorf("flows-only user: %d %q", code, body)
	}

	// Holding exactly the export permissions (without admin) is enough;
	// the config map needs configmap:edit too.
	dst.do(http.MethodPost, "/api/v1/users", `{"username":"backup","password":"Backup-Passw0rd","permissions":["flows:view","alerts:edit","snippets:edit","scripts:edit","settings:edit"],"mustChangePassword":false}`, admin)
	if code, body, _ := dst.do(http.MethodGet, "/api/v1/config/export", "", basic("backup", "Backup-Passw0rd")); code != http.StatusOK {
		t.Errorf("backup user export: %d %q", code, body)
	}
	if code, body, _ := dst.do(http.MethodGet, "/api/v1/config/export?includeConfigMap=true", "", basic("backup", "Backup-Passw0rd")); code != http.StatusForbidden || !strings.Contains(body, "configmap:edit") {
		t.Errorf("backup user export with the config map: %d %q", code, body)
	}

	// CLI: exportcfg from the source, importcfg into a third server.
	third := startComposed(t, serverconfig.Default(), io.Discard)
	file, script := filepath.Join(dir, "config.json"), filepath.Join(dir, "s.txt")
	for _, tt := range []struct {
		base, line, want string
		code             int
	}{
		{src.base, `exportcfg "` + file + `" overwriteconfigmap`, "exported 2 flows, 1 alerts, 1 snippets, 1 snippet libraries, 1 scripts, 1 settings, 1 config map entries to " + file, 0},
		{third.base, `importcfg "` + file + `" overwriteconfigmap`, "imported 2 flows (2 new, 0 replaced), 1 alerts, 1 snippets, 1 snippet libraries, 1 scripts, 1 settings from " + file + "\nreplaced the config map\ndeployed a\n", 0},
		{third.base, `importcfg "` + file + `"`, "", 2},
		{third.base, `importcfg "` + file + `" nodeploy force`, "(0 new, 2 replaced)", 0},
		{third.base, `importcfg "` + filepath.Join(dir, "none.json") + `"`, "", 2},
		{third.base, `importcfg "` + file + `" deploy`, "", 2},
		{third.base, `exportcfg "` + file + `" all`, "", 2},
		{third.base, `exportcfg "` + filepath.Join(dir, "no", "x.json") + `"`, "", 2},
	} {
		_ = os.WriteFile(script, []byte(tt.line+"\n"), 0o600)
		var out, errb bytes.Buffer
		if code := run([]string{"-a", tt.base, "-u", bootstrapAdmin, "-p", testAdminPassword, "-s", script}, strings.NewReader(""), &out, &errb); code != tt.code || !strings.Contains(out.String(), tt.want) {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", tt.line, code, out.String(), errb.String())
		}
	}
	var out, errb bytes.Buffer
	_ = os.WriteFile(script, []byte(`importcfg "`+filepath.Join(dir, "none.json")+`"`+"\n"), 0o600)
	run([]string{"-a", third.base, "-u", bootstrapAdmin, "-p", testAdminPassword, "-s", script}, strings.NewReader(""), &out, &errb)
	if !strings.Contains(errb.String(), "no such file") {
		t.Errorf("missing file: stderr %q", errb.String())
	}
}
