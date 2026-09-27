package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestFlowCLI runs every flow command in batch mode against a server, in
// order, checking output, exit code, and the resulting state.
func TestFlowCLI(t *testing.T) {
	handler, closeStore, err := buildServer(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), io.Discard, serverconfig.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = closeStore() }()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	dir := t.TempDir()
	file := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	out := t.TempDir()
	adt := file("adt.json", `{"id":"adt","name":"ADT","destinations":[{"name":"archive","type":"file","dir":"`+out+`"}]}`)
	adt2 := file("adt2.json", `{"name":"ADT v2","destinations":[{"name":"archive","type":"file","dir":"`+out+`"}]}`)
	orm := file("orm.json", `{"id":"orm","name":"Orders","dependsOn":["adt"]}`)
	all := file("all.json", `{"flows":[{"id":"orm","name":"Orders v2","dependsOn":["adt"]}]}`)
	bad := file("bad.json", `{"id":"x","status":"started"}`)
	bundle := filepath.Join(dir, "bundle.json")
	dup1 := file("dup1.json", `{"id":"dup1","name":"Same"}`)
	dup2 := file("dup2.json", `{"id":"dup2","name":"Same"}`)

	tests := []struct {
		name, script   string
		code           int
		stdout, stderr string
	}{
		{"help", "flow help", 0, "flow update-all <file>", ""},
		{"create", "flow create " + adt, 0, `"id":"adt"`, ""},
		{"create dependent", "flow create " + orm, 0, `"id":"orm"`, ""},
		{"create invalid", "flow create " + bad, 2, "", "server returned 400 Bad Request: status is managed"},
		{"create missing file", "flow create " + filepath.Join(dir, "nope.json"), 2, "", "no such file"},
		{"list", "flow list", 0, "adt\tundeployed\tADT", ""},
		{"get", "flow get adt", 0, `"name":"ADT"`, ""},
		{"get unknown", "flow get zz", 2, "", `no flow with id or name "zz"`},
		{"update", "flow update adt " + adt2, 0, `"name":"ADT v2"`, ""},
		{"update-all", "flow update-all " + all, 0, `{"updated":["orm"]}`, ""},
		{"rename", "flow rename adt ADT Inbound", 0, `"name":"ADT Inbound"`, ""},
		{"enable", "flow enable adt", 0, `"enabled":true`, ""},
		{"disable", "flow disable adt", 0, `"enabled":false`, ""},
		{"deploy with dependency", "flow deploy orm", 0, `"status":"deployed"`, ""},
		{"start", "flow start adt", 0, `"status":"started"`, ""},
		{"stop destination", "flow stop-destination adt archive", 0, `"stoppedDestinations":["archive"]`, ""},
		{"start destination", "flow start-destination adt archive", 0, `"status":"started"`, ""},
		{"pause", "flow pause adt", 0, `"status":"paused"`, ""},
		{"resume", "flow resume adt", 0, `"status":"started"`, ""},
		{"halt", "flow halt adt", 0, `"status":"halted"`, ""},
		{"stop", "flow stop adt", 0, `"status":"stopped"`, ""},
		{"invalid transition", "flow pause adt", 2, "", "server returned 409 Conflict"},
		{"redeploy-all", "flow redeploy-all", 0, `"status":"deployed"`, ""},
		{"connectors", "flow connectors", 0, `"destinations":["archive"]`, ""},
		{"ports", "flow ports", 0, `"usedBy":"api"`, ""},
		{"export", "flow export " + bundle + " orm", 0, "exported to " + bundle, ""},
		{"remove in use", "flow remove adt", 2, "", "server returned 409 Conflict"},
		{"undeploy", "flow undeploy orm", 0, `"status":"undeployed"`, ""},
		{"remove", "flow remove orm", 0, "removed orm", ""},
		// The bundle holds orm and its dependency adt, which still exists.
		{"import conflict", "flow import " + bundle, 2, "", "use overwrite=true"},
		{"import overwrite", "flow import " + bundle + " --overwrite", 0, `{"created":["orm"],"updated":["adt"]}`, ""},
		{"reserved id is not looked up as an id", "flow get export", 2, "", `no flow with id or name "export"`},
		// The import above restored orm's exported name, "Orders v2".
		{"get by name", `flow get "Orders v2"`, 0, `"id":"orm"`, ""},
		{"stats of one flow by name", `flow stats "Orders v2"`, 0, "orm\treceived=0 filtered=0 transformed=0 sent=0 errored=0 queued=0", ""},
		{"stats of every flow", "flow stats", 0, "adt\treceived=0", ""},
		{"top-level export by name", `export "Orders v2" "` + filepath.Join(dir, "spaced name.json") + `"`, 0, "exported to", ""},
		{"top-level export all", `export * "` + bundle + `"`, 0, "exported to " + bundle, ""},
		{"top-level import force", `import "` + bundle + `" force`, 0, `"updated":["adt","orm"]`, ""},
		{"top-level import usage", `import "` + bundle + `" please`, 2, "", `usage: import "path" [force]`},
		{"top-level export usage", "export orm", 2, "", `usage: export id|"name"|* "path"`},
		{"unterminated quote", `flow get "adt`, 2, "", "unterminated quote"},
		{"ambiguous name", "flow create " + dup1 + "\nflow create " + dup2 + "\nflow get Same", 2, "", `flow name "Same" is ambiguous (ids dup1, dup2)`},
		// Only enabled flows deploy: orm (which deploys adt, its dependency).
		// The forced import above restored orm as disabled.
		{"deploy all enabled", "flow undeploy adt\nflow enable orm\ndeploy 30", 0, "deployed orm\ndeployed 1 flows", ""},
		{"disabled flows stay undeployed", "flow get dup1", 0, `"status":"undeployed"`, ""},
		{"dependency deployed", "flow get adt", 0, `"status":"deployed"`, ""},
		{"deploy with nothing to do", "deploy", 0, "deployed 0 flows", ""},
		{"deploy usage", "deploy soon", 2, "", "timeout must be a positive number"},
		{"list ignores extra words", "flow list --all", 0, "adt\t", ""},
		{"rename keeps enabled", "flow enable orm\nflow rename orm Orders v3", 0, `"name":"Orders v3"`, ""},
		{"renamed flow still enabled", "flow get orm", 0, `"enabled":true`, ""},
		{"usage", "flow get", 2, "", "usage:"},
		{"unknown subcommand", "flow explode adt", 2, "", "usage:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := file("script.txt", tt.script+"\n")
			var stdout, stderr bytes.Buffer
			code := run([]string{"-a", ts.URL, "-u", bootstrapAdmin, "-p", testAdminPassword, "-s", script}, strings.NewReader(""), &stdout, &stderr)
			if code != tt.code || !strings.Contains(stdout.String(), tt.stdout) || !strings.Contains(stderr.String(), tt.stderr) {
				t.Errorf("%s: exit %d, stdout %q, stderr %q; want exit %d, stdout with %q, stderr with %q",
					tt.script, code, stdout.String(), stderr.String(), tt.code, tt.stdout, tt.stderr)
			}
		})
	}
	data, err := os.ReadFile(bundle)
	if err != nil || !strings.Contains(string(data), `"flows"`) || strings.Contains(string(data), `"status"`) {
		t.Errorf("export file = %s, %v", data, err)
	}
}
