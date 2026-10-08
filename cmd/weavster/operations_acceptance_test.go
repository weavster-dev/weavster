package main

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/state"
)

// TestOperationsDoc: docs/operations.md matches the binary. The liveness
// probe it shows answers without a login; every startup error in its
// troubleshooting table is what the server prints (with exit 1) for that
// mistake; and every log line in its table is one the server logs.
func TestOperationsDoc(t *testing.T) {
	page := docsPage(t, "operations.md")

	addr := freeAddr(t)
	stop := startCLI(t, []string{"server", "--config", writeConfig(t, "listen: {address: \""+addr+"\"}\n")}, "http://"+addr+"/api/openapi.yaml")
	resp, err := http.Get("http://" + addr + "/api/openapi.yaml")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Errorf("the liveness probe without a login: %v %v", resp, err)
	}
	if resp != nil {
		_ = resp.Body.Close()
	}
	stop()
	if !strings.Contains(page, "curl -fsS -o /dev/null http://127.0.0.1:8080/api/openapi.yaml") {
		t.Error("operations.md does not show the liveness probe")
	}

	// serve runs the server with this configuration and returns its stderr,
	// failing unless it exits 1.
	serve := func(t *testing.T, yaml string) string {
		t.Helper()
		var stderr bytes.Buffer
		if code := run([]string{"server", "--config", writeConfig(t, yaml)}, strings.NewReader(""), io.Discard, &stderr); code != 1 {
			t.Errorf("exit %d, want 1: %s", code, stderr.String())
		}
		return stderr.String()
	}
	// Each mistake, by a phrase of its row in the table.
	mistakes := map[string]func(t *testing.T) string{
		"address already in use": func(t *testing.T) string {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = ln.Close() }()
			return serve(t, "listen: {address: \""+ln.Addr().String()+"\"}\n")
		},
		"yaml: unmarshal errors": func(t *testing.T) string {
			return serve(t, "listen: {address: \""+freeAddr(t)+"\"}\nstore: {dialekt: postgres}\n")
		},
		"needs store.dsn or store.dsnEnv": func(t *testing.T) string {
			return serve(t, "listen: {address: \""+freeAddr(t)+"\"}\nstore: {dialect: postgres}\n")
		},
		"is not set, and there is no file": func(t *testing.T) string {
			return serve(t, "listen: {address: \""+freeAddr(t)+"\"}\nstore: {dialect: postgres, dsnEnv: WEAVSTER_TEST_NO_SUCH_DSN, maxRetry: 0}\nsecrets: {dir: \""+t.TempDir()+"\"}\n")
		},
		"giving up after N attempts: …": func(t *testing.T) string {
			closed := freeAddr(t) // nothing listens there
			return serve(t, "listen: {address: \""+freeAddr(t)+"\"}\nstore: {dialect: postgres, dsn: \"postgres://weavster@"+closed+"/weavster?sslmode=disable\", maxRetry: 0}\n")
		},
		"newer than this release supports": func(t *testing.T) string {
			// TestRefusesNewerSchema (PostgreSQL) makes the server refuse a
			// newer database and checks it prints this, prefix included.
			return "Error: store: " + (&state.NewerSchemaError{Version: 16, Supported: 15, WrittenBy: "1.3.0"}).Error()
		},
		"tls: open": func(t *testing.T) string {
			missing := filepath.Join(t.TempDir(), "missing.pem")
			return serve(t, "listen: {address: \"\", tlsAddress: \""+freeAddr(t)+"\"}\ntls: {certFile: \""+missing+"\", keyFile: \""+missing+"\"}\n")
		},
		"privileged OS account": func(t *testing.T) string {
			t.Setenv("WEAVSTER_ALLOW_ROOT", "")
			saved := isPrivileged
			isPrivileged = func() bool { return true }
			defer func() { isPrivileged = saved }()
			return serve(t, "listen: {address: \""+freeAddr(t)+"\"}\n")
		},
	}
	// between is the page from one heading up to the next text.
	between := func(from, to string) string {
		t.Helper()
		i := strings.Index(page, from)
		j := strings.Index(page[max(i, 0):], to)
		if i < 0 || j < 0 {
			t.Fatalf("operations.md lacks %q or, after it, %q", from, to)
		}
		return page[i : i+j]
	}
	section := between("## Troubleshoot", "\nWhen the server runs")
	rows := regexp.MustCompile("(?m)^\\| `(Error: [^`]*)` \\|").FindAllStringSubmatch(section, -1)
	if len(rows) != len(mistakes) {
		t.Errorf("the troubleshooting table has %d errors, the test %d", len(rows), len(mistakes))
	}
	placeholder := regexp.MustCompile(`\b(NAME|N|X|M)\b`)
	for _, row := range rows {
		quoted := row[1]
		var name string
		for phrase := range mistakes {
			if strings.Contains(quoted, phrase) {
				name = phrase
			}
		}
		if name == "" {
			t.Errorf("no test makes the mistake behind %s", quoted)
			continue
		}
		// "…" is any text, NAME/N/X/M any word; the address and the
		// default secrets.dir are this test's own.
		pattern := regexp.QuoteMeta(quoted)
		pattern = strings.ReplaceAll(pattern, "…", ".*")
		pattern = strings.ReplaceAll(pattern, `127\.0\.0\.1:8080`, `\S+`)
		pattern = strings.ReplaceAll(pattern, "/run/secrets/", `\S*/`)
		pattern = placeholder.ReplaceAllString(pattern, `\S+`)
		want := regexp.MustCompile("(?s)" + pattern)
		t.Run(name, func(t *testing.T) {
			if got := mistakes[name](t); !want.MatchString(got) {
				t.Errorf("the server prints\n%s\nnot what operations.md shows:\n%s", got, quoted)
			}
		})
	}

	// Every log line the page explains is, in full, one the server logs.
	var code strings.Builder
	for _, dir := range []string{".", filepath.Join("..", "..", "internal")} {
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err == nil && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
				b, rerr := os.ReadFile(path)
				code.Write(b)
				return rerr
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	logs := between("### Logs", "## Troubleshoot")
	lines := regexp.MustCompile("(?m)^\\| `([^`]*)` \\|").FindAllStringSubmatch(logs, -1)
	if len(lines) < 5 {
		t.Fatalf("log lines not read from operations.md:\n%s", logs)
	}
	for _, line := range lines {
		if !regexp.MustCompile(`\.(?:Info|Warn|Error)\("` + regexp.QuoteMeta(line[1]) + `"`).MatchString(code.String()) {
			t.Errorf("the server never logs %q", line[1])
		}
	}
}
