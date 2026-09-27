package main

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"

	"github.com/weavster-dev/weavster/internal/gitstore"
	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestGitRemote: the server's repository is pushed to a remote, reports
// ahead/behind, and on divergent history its push is refused and pull
// makes the remote win (dropping the local commit); drift then shows the
// difference and apply from the repository takes the remote's version.
func TestGitRemote(t *testing.T) {
	admin := basic(bootstrapAdmin, testAdminPassword)
	remoteDir := filepath.Join(t.TempDir(), "config.git")
	if _, err := git.PlainInit(remoteDir, true); err != nil {
		t.Fatal(err)
	}
	cfg := serverconfig.Default()
	cfg.Store.Dialect = serverconfig.DialectSQLite
	cfg.Paths.DataDir = t.TempDir()
	cfg.Git.Path = filepath.Join(t.TempDir(), "repo")
	cfg.Git.Remote = serverconfig.GitRemote{URL: remoteDir, Username: "ci", PasswordEnv: "WEAVSTER_TEST_GIT_TOKEN"}
	t.Setenv("WEAVSTER_TEST_GIT_TOKEN", "s3cret")
	c := startComposed(t, cfg, io.Discard)

	type status struct {
		URL, Branch, Head, RemoteHead string
		Ahead, Behind                 int
	}
	call := func(method, path, body string, want int) string {
		t.Helper()
		code, out, _ := c.do(method, path, body, admin)
		if code != want {
			t.Fatalf("%s %s: %d %s", method, path, code, out)
		}
		return out
	}
	remote := func() status {
		t.Helper()
		var st status
		if err := json.Unmarshal([]byte(call(http.MethodGet, "/api/v1/git/remote", "", http.StatusOK)), &st); err != nil {
			t.Fatal(err)
		}
		return st
	}

	call(http.MethodPut, "/api/v1/scripts/deploy", `{"value":"log()"}`, http.StatusOK)
	call(http.MethodPost, "/api/v1/git/commit", `{"message":"baseline"}`, http.StatusOK)
	if st := remote(); st.Ahead != 1 || st.Behind != 0 || st.RemoteHead != "" || st.URL != remoteDir || st.Branch != "main" {
		t.Errorf("before push = %+v", st)
	}
	if out := call(http.MethodPost, "/api/v1/git/push", "", http.StatusOK); strings.Contains(out, "s3cret") {
		t.Errorf("credentials in reply: %s", out)
	}
	if st := remote(); st.Ahead != 0 || st.Behind != 0 || st.RemoteHead != st.Head {
		t.Errorf("after push = %+v", st)
	}

	// A teammate changes the script in their clone and pushes.
	team, err := gitstore.OpenOrInit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := gitstore.Remote{URL: remoteDir}
	if _, err := team.PullRemoteWins(r); err != nil {
		t.Fatal(err)
	}
	if err := team.WriteFile("scripts/deploy.yaml", []byte("version: \"1\"\nscripts:\n    deploy: log(team)\n")); err != nil {
		t.Fatal(err)
	}
	teamHead, err := team.Commit("team change", gitstore.Author{Name: "teammate"})
	if err != nil {
		t.Fatal(err)
	}
	if err := team.PushTo(r); err != nil {
		t.Fatal(err)
	}

	// Meanwhile the server's configuration changes and is committed: the
	// histories diverge, so the push is refused.
	call(http.MethodPut, "/api/v1/scripts/deploy", `{"value":"log(local)"}`, http.StatusOK)
	var local struct{ Head string }
	_ = json.Unmarshal([]byte(call(http.MethodPost, "/api/v1/git/commit", `{"message":"local change"}`, http.StatusOK)), &local)
	if out := call(http.MethodPost, "/api/v1/git/push", "", http.StatusConflict); !strings.Contains(out, "pull first") {
		t.Errorf("divergent push = %s", out)
	}
	if st := remote(); st.Ahead != 1 || st.Behind != 1 || st.RemoteHead != teamHead {
		t.Errorf("divergent status = %+v", st)
	}

	// Pull: the remote wins and the local commit is reported as dropped.
	var pulled struct {
		Head    string
		Dropped []string
	}
	_ = json.Unmarshal([]byte(call(http.MethodPost, "/api/v1/git/pull", "", http.StatusOK)), &pulled)
	if pulled.Head != teamHead || len(pulled.Dropped) != 1 || pulled.Dropped[0] != local.Head {
		t.Errorf("pull = %+v (local %s)", pulled, local.Head)
	}
	if out := call(http.MethodGet, "/api/v1/git/content?path=scripts/deploy.yaml", "", http.StatusOK); !strings.Contains(out, "log(team)") {
		t.Errorf("repository after pull = %s", out)
	}

	// The live configuration did not change; drift shows it, and applying
	// the repository takes the team's version.
	var drift struct {
		Drifted bool
		Plan    struct {
			Fingerprint string
			Updated     []string
		}
	}
	_ = json.Unmarshal([]byte(call(http.MethodGet, "/api/v1/git/drift", "", http.StatusOK)), &drift)
	if !drift.Drifted || strings.Join(drift.Plan.Updated, ",") != "script/deploy" {
		t.Errorf("drift after pull = %+v", drift)
	}
	call(http.MethodPost, "/api/v1/config/apply?gitRev=&fingerprint="+drift.Plan.Fingerprint, "", http.StatusOK)
	if out := call(http.MethodGet, "/api/v1/scripts/deploy", "", http.StatusOK); !strings.Contains(out, "log(team)") {
		t.Errorf("live script = %s", out)
	}
	// In sync: nothing to commit, and the remote matches.
	if out := call(http.MethodPost, "/api/v1/git/commit", `{"message":"noop"}`, http.StatusOK); !strings.Contains(out, `"committed":false`) {
		t.Errorf("commit after apply = %s", out)
	}
	call(http.MethodPost, "/api/v1/git/push", "", http.StatusOK)
	if st := remote(); st.Ahead != 0 || st.Behind != 0 {
		t.Errorf("final status = %+v", st)
	}

	// Push and pull need git:commit.
	c.do(http.MethodPost, "/api/v1/users", `{"username":"viewer","password":"View-Passw0rd-1","permissions":["git:view"],"mustChangePassword":false}`, admin)
	viewer := basic("viewer", "View-Passw0rd-1")
	if code, _, _ := c.do(http.MethodGet, "/api/v1/git/remote", "", viewer); code != http.StatusOK {
		t.Errorf("viewer status: %d", code)
	}
	for _, p := range []string{"/api/v1/git/push", "/api/v1/git/pull"} {
		if code, body, _ := c.do(http.MethodPost, p, "", viewer); code != http.StatusForbidden || !strings.Contains(body, "git:commit") {
			t.Errorf("viewer %s: %d %s", p, code, body)
		}
	}

	// Without a remote, or with an unreachable one.
	noRemote := cfg
	noRemote.Git = serverconfig.Git{Path: filepath.Join(t.TempDir(), "other")}
	nr := startComposed(t, noRemote, io.Discard)
	if code, body, _ := nr.do(http.MethodGet, "/api/v1/git/remote", "", admin); code != http.StatusConflict || !strings.Contains(body, "no remote configured") {
		t.Errorf("no remote: %d %s", code, body)
	}
	gone := noRemote
	gone.Git.Remote = serverconfig.GitRemote{URL: filepath.Join(t.TempDir(), "missing.git")}
	g := startComposed(t, gone, io.Discard)
	if code, body, _ := g.do(http.MethodGet, "/api/v1/git/remote", "", admin); code != http.StatusBadGateway || !strings.Contains(body, "missing.git") {
		t.Errorf("unreachable remote: %d %s", code, body)
	}
}
