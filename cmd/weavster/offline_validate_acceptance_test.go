package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigValidateOffline: config-as-code files are checked on this
// machine, with no server and no database (#107 D-55) — both the shell
// command and `weavster config validate FILE...`.
func TestConfigValidateOffline(t *testing.T) {
	dir := t.TempDir()
	good, bad, missing := filepath.Join(dir, "good.yaml"), filepath.Join(dir, "bad.yaml"), filepath.Join(dir, "none.yaml")
	for path, doc := range map[string]string{
		good: "version: \"1\"\nscripts:\n  deploy: log()\nsettings:\n  retention: {days: 30}\n",
		bad:  "version: \"1\"\nsnippets:\n  pid: {name: pid, library: none, code: x}\n",
	} {
		if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	unreachable := "http://127.0.0.1:1" // nothing listens here

	// The shell command in batch mode, with no server to talk to.
	script := filepath.Join(dir, "s.txt")
	if err := os.WriteFile(script, []byte(`config validate "`+good+`"`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := run([]string{"-a", unreachable, "-s", script}, strings.NewReader(""), &out, &errb); code != 0 ||
		out.String() != good+" is valid: 0 flows, 0 alerts, 0 snippets, 0 snippet libraries, 1 scripts, 0 config map entries, 1 settings\n" {
		t.Errorf("shell validate: %d %q %q", code, out.String(), errb.String())
	}

	for _, tt := range []struct {
		name     string
		args     []string
		code     int
		out, err string
	}{
		{"valid", []string{"config", "validate", good}, 0, good + " is valid: 0 flows", ""},
		{"invalid", []string{"config", "validate", good, bad}, 1, good + " is valid", "Error: " + bad + ": config: snippets.pid: library \"none\" is not in snippetLibraries"},
		{"unreadable", []string{"config", "validate", missing, bad}, 2, "", "no such file"},
		{"no file", []string{"config", "validate"}, 2, "", "usage: weavster config validate FILE..."},
		{"needs a server", []string{"config", "diff", good}, 2, "", "config diff, plan, and apply need a server"},
		{"help", []string{"config", "-h"}, 0, "Usage: weavster config validate FILE...", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			code := run(tt.args, strings.NewReader(""), &out, &errb)
			if code != tt.code || !strings.Contains(out.String(), tt.out) || !strings.Contains(errb.String(), tt.err) {
				t.Errorf("exit %d, stdout %q, stderr %q", code, out.String(), errb.String())
			}
		})
	}
}
