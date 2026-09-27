package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/weavster-dev/weavster/internal/config"
	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestGitRepository: the live configuration is committed to the server's
// repository one config-as-code document per artifact (never the config
// map), unchanged configuration makes no commit, deletions are committed,
// and log, file history, content-at-revision, and repository info read it
// back, also after a restart.
func TestGitRepository(t *testing.T) {
	admin := basic(bootstrapAdmin, testAdminPassword)
	cfg := serverconfig.Default()
	cfg.Store.Dialect = serverconfig.DialectSQLite
	cfg.Paths.DataDir = t.TempDir()
	cfg.Git.Path = filepath.Join(t.TempDir(), "config-repo")
	c := startComposed(t, cfg, io.Discard)
	createFlow(t, c, `{"id":"adt","name":"ADT","destinations":[{"name":"out","type":"file","dir":"`+t.TempDir()+`"}]}`)
	for _, s := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/snippet-libraries", `{"name":"hl7"}`},
		{http.MethodPost, "/api/v1/snippets", `{"name":"pid","library":"hl7","code":"x"}`},
		{http.MethodPost, "/api/v1/alerts", `{"id":"errors","name":"Errors","trigger":{"events":["message.errored"]},"actions":[{"type":"email","to":["ops@example.com"]}]}`},
		{http.MethodPut, "/api/v1/scripts/deploy", `{"value":"log()"}`},
		{http.MethodPut, "/api/v1/settings/retention", `{"value":{"days":30,"maxBytes":9007199254740993,"zip":"1"}}`},
		{http.MethodPut, "/api/v1/configmap/region", `{"value":"eu"}`},
	} {
		if code, body, _ := c.do(s.method, s.path, s.body, admin); code >= 300 {
			t.Fatalf("seed %s: %d %s", s.path, code, body)
		}
	}
	type result struct {
		Committed bool
		Head      string
		Changed   []string
	}
	commit := func(message string) result {
		t.Helper()
		code, body, _ := c.do(http.MethodPost, "/api/v1/git/commit", `{"message":"`+message+`"}`, admin)
		var r result
		if err := json.Unmarshal([]byte(body), &r); code != http.StatusOK || err != nil {
			t.Fatalf("commit: %d %s", code, body)
		}
		return r
	}
	content := func(path, rev string) (int, string, http.Header) {
		return c.do(http.MethodGet, "/api/v1/git/content?path="+path+"&rev="+rev, "", admin)
	}

	if _, body, _ := c.do(http.MethodGet, "/api/v1/git", "", admin); body != `{"path":"`+cfg.Git.Path+`","branch":"main","head":""}`+"\n" {
		t.Errorf("empty repository info = %s", body)
	}
	first := commit("initial")
	want := "alerts/errors.yaml,flows/adt.yaml,scripts/deploy.yaml,settings/retention.yaml,snippetLibraries/hl7.yaml,snippets/pid.yaml"
	if !first.Committed || strings.Join(first.Changed, ",") != want {
		t.Fatalf("first commit = %+v", first)
	}
	// Every file is a config document holding its artifact, and together
	// they are a valid configuration (a snippet's library is in another
	// file).
	whole := map[string]map[string]any{}
	for _, f := range first.Changed {
		code, doc, h := content(f, "")
		if code != http.StatusOK || h.Get("Content-Type") != "application/yaml" {
			t.Fatalf("%s: %d %s", f, code, doc)
		}
		parsed, err := config.Parse([]byte(doc))
		if err != nil || len(parsed.Artifacts()) != 1 {
			t.Errorf("%s is not a one-artifact config document: %v\n%s", f, err, doc)
		}
		var sections map[string]any
		if err := yaml.Unmarshal([]byte(doc), &sections); err != nil {
			t.Fatal(err)
		}
		for section, v := range sections {
			if m, ok := v.(map[string]any); ok {
				if whole[section] == nil {
					whole[section] = map[string]any{}
				}
				for k, a := range m {
					whole[section][k] = a
				}
			}
		}
	}
	merged, _ := yaml.Marshal(whole)
	if _, err := config.ParseValid(append([]byte("version: \"1\"\n"), merged...)); err != nil {
		t.Errorf("the repository is not a valid configuration: %v", err)
	}
	if _, doc, _ := content("settings/retention.yaml", "HEAD"); !strings.Contains(doc, "maxBytes: 9007199254740993") || !strings.Contains(doc, `zip: "1"`) {
		t.Errorf("settings file = %s", doc)
	}
	if _, doc, _ := content("scripts/deploy.yaml", first.Head[:8]); doc != "version: \"1\"\nscripts:\n    deploy: log()\n" {
		t.Errorf("script file = %q", doc)
	}

	// Unchanged configuration: no new commit.
	if again := commit("nothing"); again.Committed || again.Head != first.Head || len(again.Changed) != 0 {
		t.Errorf("unchanged commit = %+v", again)
	}

	// Deleting a flow and changing a script is one commit; files outside
	// the configuration directories are kept.
	if err := os.WriteFile(filepath.Join(cfg.Git.Path, "README.md"), []byte("ops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, body, _ := c.do(http.MethodDelete, "/api/v1/flows/adt", "", admin); code != http.StatusNoContent {
		t.Fatalf("delete flow: %d %s", code, body)
	}
	c.do(http.MethodPut, "/api/v1/scripts/deploy", `{"value":"log(2)"}`, admin)
	second := commit("second")
	if !second.Committed || strings.Join(second.Changed, ",") != "README.md,flows/adt.yaml,scripts/deploy.yaml" {
		t.Errorf("second commit = %+v", second)
	}

	var log []struct{ Hash, Message, Author string }
	_, body, _ := c.do(http.MethodGet, "/api/v1/git/log", "", admin)
	if err := json.Unmarshal([]byte(body), &log); err != nil || len(log) != 2 || log[0].Hash != second.Head || log[0].Message != "second" || log[1].Author != bootstrapAdmin {
		t.Errorf("log = %s", body)
	}
	if _, body, _ := c.do(http.MethodGet, "/api/v1/git/log?path=flows/adt.yaml&limit=1", "", admin); !strings.Contains(body, second.Head) || strings.Contains(body, first.Head) {
		t.Errorf("file history = %s", body)
	}
	if code, doc, _ := content("flows/adt.yaml", "HEAD~1"); code != http.StatusOK || !strings.Contains(doc, "name: ADT") {
		t.Errorf("deleted flow at HEAD~1 = %d %s", code, doc)
	}
	for _, tt := range []struct {
		path, rev string
		status    int
		want      string
	}{
		{"flows/adt.yaml", "HEAD", http.StatusNotFound, "not found in the repository"},
		{"flows/adt.yaml", "nope", http.StatusNotFound, "not found in the repository"},
		{"", "HEAD", http.StatusBadRequest, "path is required"},
	} {
		if code, body, _ := content(tt.path, tt.rev); code != tt.status || !strings.Contains(body, tt.want) {
			t.Errorf("content %s@%s = %d %s", tt.path, tt.rev, code, body)
		}
	}

	// A git:view user reads but cannot commit.
	c.do(http.MethodPost, "/api/v1/users", `{"username":"auditor","password":"Audit-Passw0rd-1","permissions":["git:view"],"mustChangePassword":false}`, admin)
	auditor := basic("auditor", "Audit-Passw0rd-1")
	if code, _, _ := c.do(http.MethodGet, "/api/v1/git/log", "", auditor); code != http.StatusOK {
		t.Errorf("auditor log: %d", code)
	}
	if code, body, _ := c.do(http.MethodPost, "/api/v1/git/commit", `{"message":"x"}`, auditor); code != http.StatusForbidden || !strings.Contains(body, "git:commit") {
		t.Errorf("auditor commit: %d %s", code, body)
	}

	// A restarted server opens the same repository.
	restarted := startComposed(t, cfg, io.Discard)
	if _, body, _ := restarted.do(http.MethodGet, "/api/v1/git", "", admin); !strings.Contains(body, `"head":"`+second.Head+`"`) {
		t.Errorf("after restart = %s", body)
	}
	// Without git.path the repository endpoints answer 503.
	off := startComposed(t, serverconfig.Default(), io.Discard)
	if code, body, _ := off.do(http.MethodGet, "/api/v1/git", "", admin); code != http.StatusServiceUnavailable || !strings.Contains(body, "git.path") {
		t.Errorf("git off: %d %s", code, body)
	}
}
