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

	"github.com/weavster-dev/weavster/internal/gitstore"
	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestGitPlanApplyDrift: the committed repository is a plan/apply source
// and drift is detected on demand (API and CLI): live changes show as
// drift, applying from the repository restores the committed state, and
// the config map is never managed from the repository.
func TestGitPlanApplyDrift(t *testing.T) {
	admin := basic(bootstrapAdmin, testAdminPassword)
	cfg := serverconfig.Default()
	cfg.Store.Dialect = serverconfig.DialectSQLite
	cfg.Paths.DataDir = t.TempDir()
	cfg.Git.Path = filepath.Join(t.TempDir(), "repo")
	c := startComposed(t, cfg, io.Discard)
	createFlow(t, c, `{"id":"adt","destinations":[{"name":"out","type":"file","dir":"`+t.TempDir()+`"}]}`)
	for _, s := range []struct{ method, path, body string }{
		{http.MethodPut, "/api/v1/scripts/deploy", `{"value":"log()"}`},
		{http.MethodPut, "/api/v1/settings/retention", `{"value":{"days":30}}`},
		{http.MethodPut, "/api/v1/configmap/region", `{"value":"eu"}`},
		{http.MethodPost, "/api/v1/git/commit", `{"message":"baseline"}`},
	} {
		if code, body, _ := c.do(s.method, s.path, s.body, admin); code >= 300 {
			t.Fatalf("%s: %d %s", s.path, code, body)
		}
	}
	type drift struct {
		Rev     string
		Commit  string
		Drifted bool
		Plan    struct {
			Fingerprint             string
			Added, Updated, Removed []string
		}
	}
	getDrift := func(q string) drift {
		t.Helper()
		code, body, _ := c.do(http.MethodGet, "/api/v1/git/drift"+q, "", admin)
		var d drift
		if err := json.Unmarshal([]byte(body), &d); code != http.StatusOK || err != nil {
			t.Fatalf("drift%s: %d %s", q, code, body)
		}
		return d
	}
	cli := func(line string) (int, string, string) {
		t.Helper()
		script := filepath.Join(t.TempDir(), "s.txt")
		if err := os.WriteFile(script, []byte(line+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var out, errb bytes.Buffer
		code := run([]string{"-a", c.base, "-u", bootstrapAdmin, "-p", testAdminPassword, "-s", script}, strings.NewReader(""), &out, &errb)
		return code, out.String(), errb.String()
	}

	// Right after the commit there is no drift; the config map (never
	// committed) does not count.
	_, info, _ := c.do(http.MethodGet, "/api/v1/git", "", admin)
	var repoInfo struct{ Head string }
	_ = json.Unmarshal([]byte(info), &repoInfo)
	if d := getDrift(""); d.Drifted || d.Rev != "HEAD" || d.Commit != repoInfo.Head {
		t.Errorf("after commit = %+v (head %s)", d, repoInfo.Head)
	}
	if code, out, _ := cli("config drift"); code != 0 || out != "no drift: the live configuration matches the repository at HEAD ("+repoInfo.Head[:12]+")\n" {
		t.Errorf("cli no drift: %d %q", code, out)
	}

	// Live changes are drift.
	c.do(http.MethodPut, "/api/v1/scripts/deploy", `{"value":"log(2)"}`, admin)
	c.do(http.MethodDelete, "/api/v1/settings/retention", "", admin)
	c.do(http.MethodPost, "/api/v1/flows", `{"id":"tmp","destinations":[{"name":"out","type":"file","dir":"`+t.TempDir()+`"}]}`, admin)
	d := getDrift("")
	if !d.Drifted || strings.Join(d.Plan.Added, ",") != "settings/retention" || strings.Join(d.Plan.Updated, ",") != "script/deploy" || strings.Join(d.Plan.Removed, ",") != "flow/tmp" {
		t.Errorf("drift = %+v", d)
	}
	// The CLI exits 1 for drift and 2 when the check itself fails.
	if code, out, errOut := cli("config drift HEAD"); code != 1 || !strings.Contains(out, "~ script/deploy") ||
		!strings.Contains(errOut, "differs from the repository at HEAD ("+repoInfo.Head[:12]+"): 3 changes") {
		t.Errorf("cli drift: %d %q %q", code, out, errOut)
	}
	if code, _, errOut := cli("config drift nope"); code != 2 || !strings.Contains(errOut, "404") {
		t.Errorf("cli drift unknown rev: %d %q", code, errOut)
	}

	// Plan from the repository, then apply that plan.
	code, body, _ := c.do(http.MethodPost, "/api/v1/config/plan?gitRev=", "", admin)
	var plan struct{ Fingerprint string }
	if err := json.Unmarshal([]byte(body), &plan); code != http.StatusOK || err != nil || plan.Fingerprint != d.Plan.Fingerprint {
		t.Fatalf("plan from git: %d %s (drift fingerprint %s)", code, body, d.Plan.Fingerprint)
	}
	if code, body, _ := c.do(http.MethodPost, "/api/v1/config/apply?gitRev=&fingerprint="+plan.Fingerprint+"&reason=restore", "", admin); code != http.StatusOK || !strings.Contains(body, `"applied":true`) {
		t.Fatalf("apply from git: %d %s", code, body)
	}
	if d := getDrift(""); d.Drifted {
		t.Errorf("after apply = %+v", d)
	}
	if _, body, _ := c.do(http.MethodGet, "/api/v1/scripts/deploy", "", admin); !strings.Contains(body, `"log()"`) {
		t.Errorf("script = %s", body)
	}
	if _, body, _ := c.do(http.MethodGet, "/api/v1/configmap/region", "", admin); !strings.Contains(body, `"eu"`) {
		t.Errorf("config map touched: %s", body)
	}

	// A repository whose files define an artifact twice is refused, naming
	// the file; an earlier revision still plans.
	repo, err := gitstore.Open(cfg.Git.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteFile("flows/extra.yaml", []byte("version: \"1\"\nscripts:\n  deploy: other\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Commit("hand edit", gitstore.Author{Name: "ops"}); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		path   string
		status int
		want   string
	}{
		{"/api/v1/git/drift", http.StatusBadRequest, "scripts/deploy.yaml: scripts.deploy is also defined in flows/extra.yaml"},
		{"/api/v1/git/drift?rev=HEAD~1", http.StatusOK, `"drifted":false`},
		{"/api/v1/git/drift?rev=nope", http.StatusNotFound, "not found in the repository"},
	} {
		if code, body, _ := c.do(http.MethodGet, tt.path, "", admin); code != tt.status || !strings.Contains(body, tt.want) {
			t.Errorf("%s: %d %s", tt.path, code, body)
		}
	}

	// Planning from the repository also needs git:view.
	c.do(http.MethodPost, "/api/v1/users", `{"username":"cfg","password":"Cfg-Passw0rd-1","permissions":["flows:view","alerts:edit","snippets:edit","scripts:edit","settings:edit","configmap:edit"],"mustChangePassword":false}`, admin)
	if code, body, _ := c.do(http.MethodPost, "/api/v1/config/plan?gitRev=HEAD~1", "", basic("cfg", "Cfg-Passw0rd-1")); code != http.StatusForbidden || !strings.Contains(body, "git:view") {
		t.Errorf("plan without git:view: %d %s", code, body)
	}
}
