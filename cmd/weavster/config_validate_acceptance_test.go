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

	"github.com/weavster-dev/weavster/internal/config"
	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// configDoc is a config-as-code document with every section.
const configDoc = `version: "1"
flows:
  adt:
    name: ADT Inbound
    destinations: [{name: out, type: file, dir: /tmp/out}]
alerts:
  errors:
    name: ADT errors
    enabled: true
    trigger: {events: [message.errored], flows: [adt]}
    actions: [{type: email, to: [ops@example.com]}]
snippetLibraries:
  hl7: {description: HL7 helpers}
snippets:
  pid: {library: hl7, code: "get('PID.3')"}
scripts:
  deploy: log('deployed')
configmap:
  region: eu
settings:
  retention: {days: 30}
`

// TestConfigValidate: config-as-code documents are checked through the API
// and the CLI; the typed artifacts in a valid document are accepted as-is by
// the matching API endpoints.
func TestConfigValidate(t *testing.T) {
	c := startComposed(t, serverconfig.Default(), io.Discard)
	admin := basic(bootstrapAdmin, testAdminPassword)
	for _, tt := range []struct {
		name, doc string
		code      int
		want      string
	}{
		{"valid YAML", configDoc, http.StatusOK, `"counts":{"flows":1,"alerts":1,"snippets":1,"snippetLibraries":1,"scripts":1,"configmap":1,"settings":1}`},
		{"valid JSON", `{"flows":{"a":{}}}`, http.StatusOK, `"flows":1`},
		{"unknown field", "alerts:\n  x: {name: X, recipients: [a]}\n", http.StatusBadRequest, "field recipients not found"},
		{"invalid alert", strings.Replace(configDoc, "message.errored", "message.sent", 1), http.StatusBadRequest, `unknown trigger event \"message.sent\"`},
		{"missing library", strings.Replace(configDoc, "library: hl7", "library: nope", 1), http.StatusBadRequest, `library \"nope\" is not in snippetLibraries`},
		{"flow runtime field", "flows:\n  a: {status: started}\n", http.StatusBadRequest, "'status' not allowed"},
	} {
		if code, body, _ := c.do(http.MethodPost, "/api/v1/config/validate", tt.doc, admin); code != tt.code || !strings.Contains(body, tt.want) {
			t.Errorf("%s: %d %q; want %d containing %q", tt.name, code, body, tt.code, tt.want)
		}
	}

	// The document's alert and snippets are the API's shapes.
	cfg, err := config.Parse([]byte(configDoc))
	if err != nil {
		t.Fatal(err)
	}
	for path, v := range map[string]any{
		"/api/v1/snippet-libraries": cfg.SnippetLibraries["hl7"], "/api/v1/snippets": cfg.Snippets["pid"], "/api/v1/alerts": cfg.Alerts["errors"],
	} {
		body, _ := json.Marshal(v)
		if code, resp, _ := c.do(http.MethodPost, path, string(body), admin); code != http.StatusCreated {
			t.Errorf("POST %s %s: %d %q", path, body, code, resp)
		}
	}

	// CLI.
	dir := t.TempDir()
	good, bad, script := filepath.Join(dir, "good.yaml"), filepath.Join(dir, "bad.yaml"), filepath.Join(dir, "s.txt")
	_ = os.WriteFile(good, []byte(configDoc), 0o600)
	_ = os.WriteFile(bad, []byte("map: {a: b}\n"), 0o600)
	for _, tt := range []struct {
		line, out, errs string
		code            int
	}{
		{`config validate "` + good + `"`, good + " is valid: 1 flows, 1 alerts, 1 snippets, 1 snippet libraries, 1 scripts, 1 config map entries, 1 settings", "", 0},
		{`config validate "` + bad + `"`, "", "field map not found", 2},
		{`config validate "` + filepath.Join(dir, "none.yaml") + `"`, "", "no such file", 2},
		{`config plan "` + good + `"`, "", "usage: config validate", 2},
	} {
		if err := os.WriteFile(script, []byte(tt.line+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var out, errb bytes.Buffer
		if code := run([]string{"-a", c.base, "-u", bootstrapAdmin, "-p", testAdminPassword, "-s", script}, strings.NewReader(""), &out, &errb); code != tt.code || !strings.Contains(out.String(), tt.out) || !strings.Contains(errb.String(), tt.errs) {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", tt.line, code, out.String(), errb.String())
		}
	}
}
