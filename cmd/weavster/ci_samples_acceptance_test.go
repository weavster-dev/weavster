package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestCISamples: the GitHub Actions and GitLab samples (docs/examples/ci)
// are valid YAML with the documented triggers; every weavster script line
// they run works through the CLI against a composed server (plan, apply,
// then nothing left to change); the connection file they write is read by
// the CLI even with quotes in the password; and the CI/CD page shows the
// files exactly.
func TestCISamples(t *testing.T) {
	dir := filepath.Join("..", "..", "docs", "examples", "ci")
	read := func(name string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	github, gitlab, doc := read("github-actions.yml"), read("gitlab-ci.yml"), read("weavster.yaml")

	var gh struct {
		On struct {
			PullRequest struct{ Paths []string } `yaml:"pull_request"`
			Push        struct{ Branches, Paths []string }
		} `yaml:"on"`
		Jobs map[string]struct {
			If          string
			Concurrency struct {
				Group            string
				CancelInProgress bool `yaml:"cancel-in-progress"`
			}
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(github), &gh); err != nil {
		t.Fatalf("github-actions.yml: %v", err)
	}
	if strings.Join(gh.On.PullRequest.Paths, ",") != "weavster.yaml" || strings.Join(gh.On.Push.Branches, ",") != "main" ||
		!strings.Contains(gh.Jobs["plan"].If, "pull_request") || !strings.Contains(gh.Jobs["apply"].If, "push") ||
		// Plans are grouped per pull request; applies share one group and
		// are never cancelled once running.
		!strings.Contains(gh.Jobs["plan"].Concurrency.Group, "github.ref") || gh.Jobs["apply"].Concurrency.Group != "weavster-apply" ||
		gh.Jobs["apply"].Concurrency.CancelInProgress {
		t.Errorf("github triggers = %+v", gh)
	}
	var glDoc map[string]yaml.Node
	if err := yaml.Unmarshal([]byte(gitlab), &glDoc); err != nil {
		t.Fatalf("gitlab-ci.yml: %v", err)
	}
	type job struct {
		Rules []struct{ If string } `yaml:"rules"`
	}
	gl := map[string]job{}
	for _, name := range []string{"plan", "apply"} {
		var j job
		node := glDoc[name]
		if err := node.Decode(&j); err != nil {
			t.Fatalf("gitlab job %s: %v", name, err)
		}
		gl[name] = j
	}
	if len(gl["plan"].Rules) != 1 || !strings.Contains(gl["plan"].Rules[0].If, "merge_request_event") ||
		len(gl["apply"].Rules) != 1 || !strings.Contains(gl["apply"].Rules[0].If, `"main"`) || !strings.Contains(gl["apply"].Rules[0].If, `"push"`) {
		t.Errorf("gitlab rules = %+v", gl)
	}

	// The weavster scripts each sample writes: printf 'config …\n…' with
	// %s filled in as the pipeline would.
	printf := regexp.MustCompile(`printf '(config [^']*)'`)
	lines := map[string][][]string{} // sample -> scripts -> lines
	for name, sample := range map[string]string{"github": github, "gitlab": gitlab} {
		for _, m := range printf.FindAllStringSubmatch(sample, -1) {
			script := strings.Split(strings.TrimSuffix(strings.ReplaceAll(m[1], "%s", "merged 0123abc"), `\n`), `\n`)
			lines[name] = append(lines[name], script)
		}
		if got := fmt.Sprint(lines[name]); len(lines[name]) != 2 || !strings.HasPrefix(got, `[[config diff "weavster.yaml"] [config apply "weavster.yaml" merged 0123abc deploy 120 flow start-all]]`) {
			t.Fatalf("%s scripts = %s", name, got)
		}
	}
	// The shell that writes the connection file, as the samples run it.
	connect := regexp.MustCompile(`(?s)umask 077\n\s*(quote\(\).*?\} > )\S+`)

	for name, scripts := range lines {
		script := scripts
		t.Run(name, func(t *testing.T) {
			auditLog := &syncBuffer{}
			handler, closeStore, err := buildServer(context.Background(), slog.New(slog.NewTextHandler(auditLog, nil)), io.Discard, serverconfig.Default())
			if err != nil {
				t.Fatal(err)
			}
			ts := httptest.NewServer(handler)
			t.Cleanup(func() { ts.Close(); _ = closeStore() })
			c := apiClient{t: t, base: ts.URL}
			work := t.TempDir()
			if err := os.WriteFile(filepath.Join(work, "weavster.yaml"), []byte(doc), 0o600); err != nil {
				t.Fatal(err)
			}
			// A CI user whose password has a quote, a backslash, and a
			// colon; the connection file is written by the sample's own
			// shell code.
			password := `It's\n: #CI-Pass1`
			body, _ := json.Marshal(map[string]any{"username": "ci", "password": password, "mustChangePassword": false,
				"permissions": []string{"flows:view", "flows:edit", "flows:deploy", "alerts:edit", "snippets:edit", "scripts:edit", "settings:edit", "configmap:edit"}})
			if code, out, _ := c.do(http.MethodPost, "/api/v1/users", string(body), basic(bootstrapAdmin, testAdminPassword)); code != http.StatusCreated {
				t.Fatalf("create ci user: %d %s", code, out)
			}
			conn := filepath.Join(work, "weavster.conn")
			sample := map[string]string{"github": github, "gitlab": gitlab}[name]
			m := connect.FindStringSubmatch(sample)
			if m == nil {
				t.Fatalf("%s: connection-file shell not found", name)
			}
			shell := exec.Command("sh", "-c", "umask 077\n"+dedent(m[1])+`"$CONN"`)
			shell.Env = append(os.Environ(), "WEAVSTER_ADDRESS="+c.base, "WEAVSTER_USER=ci", "WEAVSTER_PASSWORD="+password, "CONN="+conn)
			if out, err := shell.CombinedOutput(); err != nil {
				t.Fatalf("%s connection shell: %v %s", name, err, out)
			}
			if st, err := os.Stat(conn); err != nil || st.Mode().Perm() != 0o600 {
				t.Errorf("connection file mode = %v %v", st.Mode(), err)
			}
			runLine := func(lines []string) (int, string, string) {
				t.Helper()
				text := strings.ReplaceAll(strings.Join(lines, "\n"), `"weavster.yaml"`, `"`+filepath.Join(work, "weavster.yaml")+`"`)
				s := filepath.Join(work, "s.txt")
				if err := os.WriteFile(s, []byte(text+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				var out, errb bytes.Buffer
				code := run([]string{"-c", conn, "-s", s}, strings.NewReader(""), &out, &errb)
				return code, out.String(), errb.String()
			}
			if code, out, errOut := runLine(script[0]); code != 0 || !strings.Contains(out, "+ flow/adt") || !strings.Contains(out, "+ script/deploy") {
				t.Errorf("plan: %d %q %q", code, out, errOut)
			}
			if code, out, errOut := runLine(script[1]); code != 0 || !strings.Contains(out, "applied 3 changes") || !strings.Contains(out, "deployed adt") {
				t.Errorf("apply and deploy: %d %q %q", code, out, errOut)
			}
			if _, out, _ := c.do(http.MethodGet, "/api/v1/flows/adt", "", basic(bootstrapAdmin, testAdminPassword)); !strings.Contains(out, `"status":"started"`) {
				t.Errorf("flow after apply = %s", out)
			}
			if code, out, _ := runLine(script[0]); code != 0 || !strings.Contains(out, "no changes") {
				t.Errorf("plan after apply: %d %q", code, out)
			}
			// The commit is the reason in the audit record.
			if !strings.Contains(auditLog.String(), "query.reason:merged 0123abc") {
				t.Errorf("audit log lacks the reason")
			}
			// An invalid document fails the plan job (batch exit 2).
			if err := os.WriteFile(filepath.Join(work, "weavster.yaml"), []byte("version: \"1\"\nflows: [\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if code, _, _ := runLine(script[0]); code != 2 {
				t.Errorf("invalid document: exit %d", code)
			}
		})
	}

	page, err := os.ReadFile(filepath.Join("..", "..", "docs", "ci-cd.md"))
	if err != nil {
		t.Fatal(err)
	}
	for name, sample := range map[string]string{"github-actions.yml": github, "gitlab-ci.yml": gitlab, "weavster.yaml": doc} {
		if !strings.Contains(string(page), strings.TrimRight(sample, "\n")) {
			t.Errorf("docs/ci-cd.md does not show %s exactly", name)
		}
	}
}

// dedent removes the indentation the YAML block gave the shell lines.
func dedent(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimLeft(l, " ")
	}
	return strings.Join(lines, "\n")
}
