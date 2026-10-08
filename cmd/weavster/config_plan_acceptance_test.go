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

// TestConfigPlan: a config-as-code document is planned against a server's
// live configuration — adds, field-level updates, removals only in the
// sections the document manages — and planning changes nothing.
func TestConfigPlan(t *testing.T) {
	c := startComposed(t, serverconfig.Default(), io.Discard)
	admin := basic(bootstrapAdmin, testAdminPassword)
	for _, s := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/flows", `{"id":"a","name":"A","enabled":true}`},
		{http.MethodPost, "/api/v1/flows", `{"id":"b","name":"B"}`},
		{http.MethodPost, "/api/v1/alerts", `{"id":"errors","name":"Errors","trigger":{"events":["message.errored"]},"actions":[{"type":"email","to":["ops@example.com"]}]}`},
		{http.MethodPut, "/api/v1/configmap/region", `{"value":"us"}`},
		{http.MethodPut, "/api/v1/settings/retention", `{"value":{"days":30}}`},
		{http.MethodPost, "/api/v1/flows/a/deploy", ``},
	} {
		if code, body, _ := c.do(s.method, s.path, s.body, admin); code >= 300 {
			t.Fatalf("seed %s: %d %s", s.path, code, body)
		}
	}
	doc := `flows:
  a: {name: ADT Inbound, enabled: true}
  c: {name: C}
configmap:
  region: eu
settings: {}
`
	code, body, _ := c.do(http.MethodPost, "/api/v1/config/plan", doc, admin)
	var plan struct {
		Fingerprint             string
		Added, Updated, Removed []string
		Unchanged               int
		Changes                 []struct {
			Key, Action string
			Fields      []struct{ Path string }
		}
		Text string
	}
	if err := json.Unmarshal([]byte(body), &plan); code != http.StatusOK || err != nil {
		t.Fatalf("plan: %d %s", code, body)
	}
	// The alert section is not in the document, so alert/errors stays; flow
	// a's runtime status (deployed) is not configuration.
	if strings.Join(plan.Added, ",") != "flow/c" || strings.Join(plan.Updated, ",") != "configmap/region,flow/a" ||
		strings.Join(plan.Removed, ",") != "flow/b,settings/retention" || plan.Fingerprint == "" {
		t.Errorf("plan = %+v", plan)
	}
	for _, want := range []string{"~ flow/a\n    name: \"A\" → \"ADT Inbound\"\n", "~ configmap/region\n    value: \"us\" → \"eu\"\n", "- flow/b\n", "1 to add, 2 to change, 2 to remove, 0 unchanged\n"} {
		if !strings.Contains(plan.Text, want) {
			t.Errorf("text lacks %q:\n%s", want, plan.Text)
		}
	}
	// Nothing changed; planning again gives the same fingerprint.
	if _, m, _ := c.do(http.MethodGet, "/api/v1/configmap/region", "", admin); !strings.Contains(m, `"us"`) {
		t.Errorf("config map changed: %s", m)
	}
	if code, _, _ := c.do(http.MethodGet, "/api/v1/flows/b", "", admin); code != http.StatusOK {
		t.Error("flow b was removed by a plan")
	}
	_, again, _ := c.do(http.MethodPost, "/api/v1/config/plan", doc, admin)
	if !strings.Contains(again, `"fingerprint":"`+plan.Fingerprint+`"`) {
		t.Error("fingerprint changed without a change")
	}
	// A matching document plans no changes.
	if _, same, _ := c.do(http.MethodPost, "/api/v1/config/plan", "flows:\n  a: {name: A, enabled: true}\n  b: {name: B}\n", admin); !strings.Contains(same, `"text":"no changes\n"`) {
		t.Errorf("same = %s", same)
	}
	if code, body, _ := c.do(http.MethodPost, "/api/v1/config/plan", "bogus: 1\n", admin); code != http.StatusBadRequest || !strings.Contains(body, "field bogus not found") {
		t.Errorf("invalid: %d %s", code, body)
	}
	c.do(http.MethodPost, "/api/v1/users", `{"username":"editor","password":"Editor-Passw0rd","permissions":["flows:edit"],"mustChangePassword":false}`, admin)
	if code, body, _ := c.do(http.MethodPost, "/api/v1/config/plan", doc, basic("editor", "Editor-Passw0rd")); code != http.StatusForbidden || !strings.Contains(body, "missing permission") {
		t.Errorf("editor: %d %q", code, body)
	}

	// CLI.
	dir := t.TempDir()
	path, script := filepath.Join(dir, "weavster.yaml"), filepath.Join(dir, "s.txt")
	_ = os.WriteFile(path, []byte(doc), 0o600)
	for _, tt := range []struct {
		line, out string
		code      int
	}{
		{`config diff "` + path + `"`, "+ flow/c\n", 0},
		{`config plan "` + path + `"`, `  "added": [` + "\n" + `    "flow/c"`, 0},
		{`config drift "` + path + `"`, "", 2},
	} {
		if err := os.WriteFile(script, []byte(tt.line+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var out, errb bytes.Buffer
		if code := run([]string{"-a", c.base, "-u", bootstrapAdmin, "-p", testAdminPassword, "-s", script}, strings.NewReader(""), &out, &errb); code != tt.code || !strings.Contains(out.String(), tt.out) {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", tt.line, code, out.String(), errb.String())
		}
	}
}
