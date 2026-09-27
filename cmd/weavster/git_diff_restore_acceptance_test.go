package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestGitDiffRestore: revisions are compared (files and patch), a file and
// then the whole repository are restored as new commits, and drift → apply
// brings the live configuration back to the restored revision.
func TestGitDiffRestore(t *testing.T) {
	admin := basic(bootstrapAdmin, testAdminPassword)
	cfg := serverconfig.Default()
	cfg.Store.Dialect = serverconfig.DialectSQLite
	cfg.Paths.DataDir = t.TempDir()
	cfg.Git.Path = filepath.Join(t.TempDir(), "repo")
	c := startComposed(t, cfg, io.Discard)
	call := func(method, path, body string, want int) string {
		t.Helper()
		code, out, _ := c.do(method, path, body, admin)
		if code != want {
			t.Fatalf("%s %s: %d %s", method, path, code, out)
		}
		return out
	}
	commit := func(msg string) string {
		t.Helper()
		var r struct{ Head string }
		_ = json.Unmarshal([]byte(call(http.MethodPost, "/api/v1/git/commit", `{"message":"`+msg+`"}`, http.StatusOK)), &r)
		return r.Head
	}
	call(http.MethodPut, "/api/v1/scripts/deploy", `{"value":"log()"}`, http.StatusOK)
	call(http.MethodPut, "/api/v1/settings/retention", `{"value":{"days":30}}`, http.StatusOK)
	first := commit("baseline")
	call(http.MethodPut, "/api/v1/scripts/deploy", `{"value":"log(2)"}`, http.StatusOK)
	commit("second")
	call(http.MethodDelete, "/api/v1/settings/retention", "", http.StatusNoContent)
	commit("third")

	type diff struct {
		From, To string
		Files    []struct{ Path, Status string }
		Patch    string
	}
	var d diff
	_ = json.Unmarshal([]byte(call(http.MethodGet, "/api/v1/git/diff?from=HEAD~2", "", http.StatusOK)), &d)
	if d.To != "HEAD" || fmt.Sprint(d.Files) != "[{scripts/deploy.yaml modified} {settings/retention.yaml deleted}]" ||
		!strings.Contains(d.Patch, "-    deploy: log()\n+    deploy: log(2)") {
		t.Errorf("diff = %+v", d)
	}
	_ = json.Unmarshal([]byte(call(http.MethodGet, "/api/v1/git/diff", "", http.StatusOK)), &d)
	if len(d.Files) != 0 || d.Patch != "" {
		t.Errorf("working tree diff = %+v", d)
	}

	type result struct {
		Committed bool
		Head      string
		Changed   []string
	}
	restore := func(body string) result {
		t.Helper()
		var r result
		_ = json.Unmarshal([]byte(call(http.MethodPost, "/api/v1/git/restore", body, http.StatusOK)), &r)
		return r
	}
	type drift struct {
		Drifted bool
		Plan    struct {
			Fingerprint    string
			Added, Updated []string
		}
	}
	getDrift := func() drift {
		t.Helper()
		var dr drift
		_ = json.Unmarshal([]byte(call(http.MethodGet, "/api/v1/git/drift", "", http.StatusOK)), &dr)
		return dr
	}

	// Restore one file: a new commit; the live configuration now drifts.
	if r := restore(`{"rev":"HEAD~2","path":"scripts/deploy.yaml","message":"script back"}`); !r.Committed || fmt.Sprint(r.Changed) != "[scripts/deploy.yaml]" {
		t.Errorf("file restore = %+v", r)
	}
	if out := call(http.MethodGet, "/api/v1/git/content?path=scripts/deploy.yaml", "", http.StatusOK); !strings.Contains(out, "deploy: log()") {
		t.Errorf("restored file = %s", out)
	}
	if dr := getDrift(); !dr.Drifted || fmt.Sprint(dr.Plan.Updated) != "[script/deploy]" {
		t.Errorf("drift after file restore = %+v", dr)
	}

	// Restore the whole repository to the baseline, then apply it.
	if r := restore(`{"rev":"` + first + `","message":"back to baseline"}`); !r.Committed || fmt.Sprint(r.Changed) != "[settings/retention.yaml]" {
		t.Errorf("repository restore = %+v", r)
	}
	if r := restore(`{"rev":"` + first + `","message":"again"}`); r.Committed || len(r.Changed) != 0 {
		t.Errorf("restore without change = %+v", r)
	}
	dr := getDrift()
	if fmt.Sprint(dr.Plan.Added, dr.Plan.Updated) != "[settings/retention] [script/deploy]" {
		t.Errorf("drift after repository restore = %+v", dr)
	}
	call(http.MethodPost, "/api/v1/config/apply?gitRev=&fingerprint="+dr.Plan.Fingerprint, "", http.StatusOK)
	if dr := getDrift(); dr.Drifted {
		t.Errorf("drift after apply = %+v", dr)
	}
	if out := call(http.MethodGet, "/api/v1/scripts/deploy", "", http.StatusOK); !strings.Contains(out, `"log()"`) {
		t.Errorf("live script = %s", out)
	}
	var log []struct{ Message string }
	_ = json.Unmarshal([]byte(call(http.MethodGet, "/api/v1/git/log", "", http.StatusOK)), &log)
	if len(log) != 5 || log[0].Message != "back to baseline" {
		t.Errorf("history kept = %+v", log)
	}

	for _, tt := range []struct {
		method, path, body string
		status             int
		want               string
	}{
		{http.MethodPost, "/api/v1/git/restore", `{"rev":"nope","message":"x"}`, http.StatusNotFound, "not found"},
		{http.MethodPost, "/api/v1/git/restore", `{"rev":"HEAD","path":"flows/none.yaml","message":"x"}`, http.StatusNotFound, "not found"},
		{http.MethodGet, "/api/v1/git/diff?from=nope", "", http.StatusNotFound, "not found"},
	} {
		if code, body, _ := c.do(tt.method, tt.path, tt.body, admin); code != tt.status || !strings.Contains(body, tt.want) {
			t.Errorf("%s %s: %d %s", tt.method, tt.path, code, body)
		}
	}

	// Uncommitted files show in the working-tree diff and block a restore.
	if err := os.WriteFile(filepath.Join(cfg.Git.Path, "notes.txt"), []byte("draft"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := call(http.MethodGet, "/api/v1/git/diff", "", http.StatusOK); !strings.Contains(out, `{"path":"notes.txt","status":"added"}`) {
		t.Errorf("working tree diff = %s", out)
	}
	if code, body, _ := c.do(http.MethodPost, "/api/v1/git/restore", `{"rev":"HEAD~1","message":"x"}`, admin); code != http.StatusConflict || !strings.Contains(body, "notes.txt") {
		t.Errorf("restore over uncommitted: %d %s", code, body)
	}

	// Diff shows contents: it also needs the export permissions.
	c.do(http.MethodPost, "/api/v1/users", `{"username":"viewer","password":"View-Passw0rd-1","permissions":["git:view"],"mustChangePassword":false}`, admin)
	if code, body, _ := c.do(http.MethodGet, "/api/v1/git/diff?from=HEAD~1", "", basic("viewer", "View-Passw0rd-1")); code != http.StatusForbidden || !strings.Contains(body, "flows:view") {
		t.Errorf("viewer diff: %d %s", code, body)
	}
}
