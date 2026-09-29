package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGettingStartedDoc: docs/getting-started.md shows the example files
// exactly and runs the commands scripts/getting-started.sh runs in CI; and
// following it works: the fixture passes offline, the setup script applies,
// deploys, and starts the flow on a running server, and a message sent to
// it is transformed and sent. (CI's compose job runs the same guide against
// Docker Compose.)
func TestGettingStartedDoc(t *testing.T) {
	root := filepath.Join("..", "..")
	read := func(path string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	guide := read("docs/getting-started.md")
	for _, f := range []string{"hello.yaml", "hello.test.yaml", "setup.txt"} {
		if body := strings.TrimRight(read("examples/getting-started/"+f), "\n"); !strings.Contains(guide, "\n"+body+"\n```") {
			t.Errorf("the guide does not show examples/getting-started/%s as it is", f)
		}
	}
	script := read("scripts/getting-started.sh")
	steps := script[strings.Index(script, "# --- the guide ---"):strings.Index(script, "# --- end ---")]
	for _, line := range strings.Split(steps, "\n")[1:] {
		if line = strings.TrimSpace(line); line != "" && !strings.Contains(guide, line) {
			t.Errorf("the guide does not show the command CI runs: %s", line)
		}
	}

	// Follow the guide against a running server (on a free port, with this
	// test's admin password), from the repository root as the guide does.
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\n"+storeConfig(t))
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(wd) }()
	var out, errb bytes.Buffer
	if code := run([]string{"test", "examples/getting-started"}, strings.NewReader(""), &out, &errb); code != 0 {
		t.Fatalf("weavster test: exit %d %s", code, errb.String())
	}
	out.Reset()
	if code := run([]string{"-a", "http://" + addr, "-u", bootstrapAdmin, "-p", testAdminPassword, "-s", "examples/getting-started/setup.txt"}, strings.NewReader(""), &out, &errb); code != 0 {
		t.Fatalf("setup script: exit %d\n%s\n%s", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), "+ flow/hello\n1 to add, 0 to change, 0 to remove, 0 unchanged\napplied 1 changes\n") || !strings.Contains(out.String(), "hello\tstarted\tHello") {
		t.Errorf("setup output lacks the plan or the started flow:\n%s\nstderr: %s", out.String(), errb.String())
	}
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	code, body, _ := c.do(http.MethodPost, "/api/v1/flows/hello/messages", `{"patient":{"name":"Ada Lovelace","mrn":"12345"}}`, admin)
	var sent struct{ ID, Status string }
	if code != http.StatusAccepted || json.Unmarshal([]byte(body), &sent) != nil || sent.Status != "sent" {
		t.Fatalf("send: %d %s", code, body)
	}
	if code, content, _ := c.do(http.MethodGet, "/api/v1/messages/"+sent.ID+"/content?part=transformed", "", admin); code != http.StatusOK ||
		content != `{"greeting":{"mrn":"12345","to":"Ada Lovelace"},"patient":{"mrn":"12345","name":"Ada Lovelace"}}` {
		t.Errorf("transformed content: %d %s", code, content)
	}
	if !strings.Contains(guide, `{"greeting":{"mrn":"12345","to":"Ada Lovelace"},"patient":{"mrn":"12345","name":"Ada Lovelace"}}`) {
		t.Error("the guide shows other transformed content than the server returns")
	}
}
