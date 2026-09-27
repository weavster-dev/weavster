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
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestConfigApply: a reviewed plan is applied (flows removed dependents
// first), a stale plan is refused, a failing change rolls everything back,
// a dry run changes nothing, and each attempt is audited with its result.
func TestConfigApply(t *testing.T) {
	auditLog := &syncBuffer{}
	handler, closeStore, err := buildServer(context.Background(), slog.New(slog.NewTextHandler(auditLog, nil)), io.Discard, serverconfig.Default())
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(handler)
	t.Cleanup(func() { ts.Close(); _ = closeStore() })
	c := apiClient{t: t, base: ts.URL}
	admin := basic(bootstrapAdmin, testAdminPassword)
	for _, s := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/flows", `{"id":"base","name":"Base"}`},
		{http.MethodPost, "/api/v1/flows", `{"id":"app","name":"App","dependsOn":["base"]}`},
		{http.MethodPost, "/api/v1/snippet-libraries", `{"name":"hl7"}`},
		{http.MethodPost, "/api/v1/snippets", `{"name":"pid","library":"hl7","code":"x"}`},
		{http.MethodPut, "/api/v1/configmap/region", `{"value":"us"}`},
	} {
		if code, body, _ := c.do(s.method, s.path, s.body, admin); code >= 300 {
			t.Fatalf("seed %s: %d %s", s.path, code, body)
		}
	}
	plan := func(doc string) string {
		t.Helper()
		code, body, _ := c.do(http.MethodPost, "/api/v1/config/plan", doc, admin)
		var p struct{ Fingerprint string }
		if err := json.Unmarshal([]byte(body), &p); code != http.StatusOK || err != nil {
			t.Fatalf("plan: %d %s", code, body)
		}
		return p.Fingerprint
	}
	apply := func(doc, query string) (int, string) {
		code, body, _ := c.do(http.MethodPost, "/api/v1/config/apply"+query, doc, admin)
		return code, body
	}
	state := func() string {
		var out []string
		for _, p := range []string{"/api/v1/flows", "/api/v1/alerts", "/api/v1/snippets", "/api/v1/snippet-libraries", "/api/v1/configmap"} {
			_, body, _ := c.do(http.MethodGet, p, "", admin)
			out = append(out, body)
		}
		return strings.Join(out, "\n")
	}

	// Rollback: removing base while the document keeps app (which uses it)
	// fails after the alert was added; the alert is removed again.
	keepApp := `flows:
  app: {name: App, dependsOn: [base]}
alerts:
  errors: {name: Errors, trigger: {events: [message.errored]}, actions: [{type: email, to: [ops@example.com]}]}
`
	before := state()
	if code, body := apply(keepApp, "?fingerprint="+plan(keepApp)+"&reason=ticket+42"); code != http.StatusConflict ||
		!strings.Contains(body, "apply stopped at flow/base") || !strings.Contains(body, "every change was rolled back") {
		t.Errorf("rollback apply: %d %s", code, body)
	}
	if after := state(); after != before {
		t.Errorf("rollback left changes:\n%s\n---\n%s", before, after)
	}

	doc := `flows:
  c: {name: C}
snippets: {}
snippetLibraries: {}
configmap:
  region: eu
`
	fp := plan(doc)
	if code, body := apply(doc, "?fingerprint="+fp+"&dryRun=true"); code != http.StatusOK || !strings.Contains(body, `"applied":false`) || state() != before {
		t.Errorf("dry run: %d %s", code, body)
	}
	// Stale: the live configuration changes after the plan.
	c.do(http.MethodPut, "/api/v1/configmap/zone", `{"value":"a"}`, admin)
	if code, body := apply(doc, "?fingerprint="+fp); code != http.StatusConflict || !strings.Contains(body, "changed since the plan") {
		t.Errorf("stale apply: %d %s", code, body)
	}
	// Plan again and apply: app goes before base, the snippet before its library.
	if code, body := apply(doc, "?fingerprint="+plan(doc)); code != http.StatusOK || !strings.Contains(body, `"applied":true`) {
		t.Fatalf("apply: %d %s", code, body)
	}
	for path, want := range map[string]string{
		"/api/v1/flows": `"id":"c"`, "/api/v1/snippets": `[]`, "/api/v1/snippet-libraries": `[]`, "/api/v1/configmap": `{"region":"eu"}`,
	} {
		if _, body, _ := c.do(http.MethodGet, path, "", admin); !strings.Contains(body, want) || strings.Contains(body, `"base"`) {
			t.Errorf("after apply %s = %s", path, body)
		}
	}
	// New flows that depend on each other apply together; a document edited
	// after its plan is refused even though the server did not change.
	deps := strings.Replace(doc, "  c: {name: C}\n", "  c: {name: C}\n  a: {name: A, dependsOn: [y]}\n  y: {name: Y, dependsOn: [z]}\n  z: {name: Z}\n", 1)
	fpDeps := plan(deps)
	if code, body := apply(strings.Replace(deps, "  z: {name: Z}\n", "  z: {name: Z}\n  extra: {name: X}\n", 1), "?fingerprint="+fpDeps); code != http.StatusConflict || !strings.Contains(body, "the document changed") {
		t.Errorf("edited document: %d %s", code, body)
	}
	if code, body := apply(deps, "?fingerprint="+fpDeps); code != http.StatusOK {
		t.Errorf("dependent new flows: %d %s", code, body)
	}
	if _, body := apply(deps, "?fingerprint="+plan(deps)); !strings.Contains(body, `"text":"no changes\n"`) {
		t.Errorf("second apply = %s", body)
	}
	doc = deps
	if _, body := apply(doc, "?fingerprint="+plan(doc)); !strings.Contains(body, `"text":"no changes\n"`) {
		t.Errorf("second apply = %s", body)
	}
	for _, want := range []string{"result:rolled back", "query.reason:ticket 42", "result:dry run", "result:stale", "result:applied", "plan.removed:5: configmap/zone,flow/app,flow/base,library/hl7,snippet/pid"} {
		if !strings.Contains(auditLog.String(), want) {
			t.Errorf("audit log lacks %s", want)
		}
	}
	c.do(http.MethodPost, "/api/v1/users", `{"username":"editor","password":"Editor-Passw0rd","permissions":["flows:view","flows:edit"],"mustChangePassword":false}`, admin)
	if code, _, _ := c.do(http.MethodPost, "/api/v1/config/apply?fingerprint=x", doc, basic("editor", "Editor-Passw0rd")); code != http.StatusForbidden {
		t.Errorf("editor: %d", code)
	}

	// CLI.
	dir := t.TempDir()
	path, script := filepath.Join(dir, "weavster.yaml"), filepath.Join(dir, "s.txt")
	_ = os.WriteFile(path, []byte("configmap:\n  region: ap\n"), 0o600)
	for _, tt := range []struct {
		line, out string
		code      int
	}{
		{`config apply "` + path + `" --dry-run`, "~ configmap/region\n    value: \"eu\" → \"ap\"\n0 to add, 1 to change", 0},
		{`config apply "` + path + `" "rotate region"`, "applied 1 changes\n", 0},
		{`config apply "` + path + `"`, "no changes\n", 0},
		{`config apply "` + path + `" --dry-run`, "no changes\ndry run: the plan is current; nothing was changed\n", 0},
		{`config apply "` + filepath.Join(dir, "none.yaml") + `"`, "", 2},
		{`config apply "` + path + `" --dryrun`, "", 2},
	} {
		if err := os.WriteFile(script, []byte(tt.line+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var out, errb bytes.Buffer
		if code := run([]string{"-a", c.base, "-u", bootstrapAdmin, "-p", testAdminPassword, "-s", script}, strings.NewReader(""), &out, &errb); code != tt.code || !strings.Contains(out.String(), tt.out) {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", tt.line, code, out.String(), errb.String())
		}
	}
	if _, body, _ := c.do(http.MethodGet, "/api/v1/configmap", "", admin); body != `{"region":"ap"}`+"\n" {
		t.Errorf("after CLI apply: %s", body)
	}
}
