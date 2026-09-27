package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
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
		Jobs map[string]struct{ If string } `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(github), &gh); err != nil {
		t.Fatalf("github-actions.yml: %v", err)
	}
	if strings.Join(gh.On.PullRequest.Paths, ",") != "weavster.yaml" || strings.Join(gh.On.Push.Branches, ",") != "main" ||
		!strings.Contains(gh.Jobs["plan"].If, "pull_request") || !strings.Contains(gh.Jobs["apply"].If, "push") {
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
		len(gl["apply"].Rules) != 1 || !strings.Contains(gl["apply"].Rules[0].If, `"main"`) {
		t.Errorf("gitlab rules = %+v", gl)
	}

	// The weavster script lines each sample writes: printf 'config …\n'
	// with %s filled in as the pipeline would.
	printf := regexp.MustCompile(`printf '(config [^']*)\\n'`)
	lines := map[string][]string{}
	for name, sample := range map[string]string{"github": github, "gitlab": gitlab} {
		for _, m := range printf.FindAllStringSubmatch(sample, -1) {
			lines[name] = append(lines[name], strings.ReplaceAll(m[1], "%s", "merged 0123abc"))
		}
		if len(lines[name]) != 2 || !strings.HasPrefix(lines[name][0], "config diff ") || !strings.HasPrefix(lines[name][1], "config apply ") {
			t.Fatalf("%s script lines = %q", name, lines[name])
		}
	}

	for name, script := range lines {
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
			// The connection file as the samples write it: single-quoted,
			// quotes doubled.
			pw := testAdminPassword
			conn := filepath.Join(work, "weavster.conn")
			quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }
			if err := os.WriteFile(conn, []byte("address: "+quote(c.base)+"\nuser: "+quote(bootstrapAdmin)+"\npassword: "+quote(pw)+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			runLine := func(line string) (int, string, string) {
				t.Helper()
				line = strings.ReplaceAll(line, `"weavster.yaml"`, `"`+filepath.Join(work, "weavster.yaml")+`"`)
				s := filepath.Join(work, "s.txt")
				if err := os.WriteFile(s, []byte(line+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				var out, errb bytes.Buffer
				code := run([]string{"-c", conn, "-s", s}, strings.NewReader(""), &out, &errb)
				return code, out.String(), errb.String()
			}
			if code, out, errOut := runLine(script[0]); code != 0 || !strings.Contains(out, "+ flow/adt") || !strings.Contains(out, "+ script/deploy") {
				t.Errorf("plan: %d %q %q", code, out, errOut)
			}
			if code, out, errOut := runLine(script[1]); code != 0 || !strings.Contains(out, "applied 3 changes") {
				t.Errorf("apply: %d %q %q", code, out, errOut)
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

	// A password with a quote survives the samples' quoting.
	var parsed struct{ Password string }
	if err := yaml.Unmarshal([]byte("password: 'it''s: #fine'\n"), &parsed); err != nil || parsed.Password != "it's: #fine" {
		t.Errorf("quoted password = %q %v", parsed.Password, err)
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
