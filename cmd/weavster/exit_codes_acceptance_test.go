package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestExitCodesAndDeprecatedCommands: the binary's exit codes follow #107 D-16/D-45
// (server: 0 help, 2 usage, 1 failure; shell: 0 success, 2 any error), errors
// start with "Error:", and deprecated command names run with a warning that
// names the replacement.
func TestExitCodesAndDeprecatedCommands(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"server help", []string{"server", "-h"}, 0, ""},
		{"client --help", []string{"--help"}, 0, ""},
		{"test help", []string{"test", "-h"}, 0, ""},
		{"server unknown flag", []string{"server", "--nope"}, 2, "Error: flag provided but not defined: -nope"},
		{"server extra arguments", []string{"server", "127.0.0.1:0", "extra"}, 2, "Error: unexpected arguments"},
		{"server missing config", []string{"server", "--config", filepath.Join(t.TempDir(), "none.yaml")}, 1, "Error: config:"},
		{"client unknown flag", []string{"--nope"}, 2, "Error: flag provided but not defined: -nope"},
		{"client missing connection file", []string{"-c", filepath.Join(t.TempDir(), "none.yaml")}, 2, "Error:"},
		{"test unknown flag", []string{"test", "--nope"}, 2, "Error: flag provided but not defined: -nope"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			if code := run(tt.args, strings.NewReader(""), &out, &errb); code != tt.code || !strings.Contains(errb.String(), tt.want) {
				t.Errorf("exit %d, stderr %q; want %d containing %q", code, errb.String(), tt.code, tt.want)
			}
		})
	}

	c := startComposed(t, serverconfig.Default(), io.Discard)
	createFlow(t, c, `{"id":"adt"}`)
	script := filepath.Join(t.TempDir(), "s.txt")
	for _, tt := range []struct {
		name, lines   string
		code          int
		stdout, errs  string
		noStderrError bool
	}{
		{"deprecated channel runs as flow", "channel list\n", 0, "adt", `Warning: "channel" is deprecated; use "flow"`, true},
		{"deprecated codetemplate runs as snippet", "codetemplate list\n", 0, "", `Warning: "codetemplate" is deprecated; use "snippet"`, true},
		{"deprecated name keeps usage errors", "channel\n", 2, "", `Warning: "channel" is deprecated`, false},
		{"unknown command", "bogus\n", 2, "", `Error: unknown command "bogus"`, false},
		{"server error text", "snippet remove nope\n", 2, "", "Error: server returned 404 Not Found: snippet not found", false},
		{"every line runs; any error exits 2", "bogus\nflow list\n", 2, "adt", "Error:", false},
		{"all succeed", "flow list\nstatus\n", 0, "adt", "", true},
		{"a line one byte over the limit ends the script", "flow list\n" + strings.Repeat("x", maxShellLine+1) + "\nflow list\n", 2, "adt", "reading the script", false},
		{"a line at the limit runs", strings.Repeat(" ", maxShellLine-9) + "flow list\n", 0, "adt", "", true},
		{"a line over the limit ends the script", "flow list\n" + strings.Repeat("x", maxShellLine+10) + "\nflow list\n", 2, "adt", "reading the script", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(script, []byte(tt.lines), 0o600); err != nil {
				t.Fatal(err)
			}
			var out, errb bytes.Buffer
			code := run([]string{"-a", c.base, "-u", bootstrapAdmin, "-p", testAdminPassword, "-s", script}, strings.NewReader(""), &out, &errb)
			if code != tt.code || !strings.Contains(out.String(), tt.stdout) || !strings.Contains(errb.String(), tt.errs) {
				t.Errorf("exit %d, stdout %q, stderr %q", code, out.String(), errb.String())
			}
			if tt.noStderrError && strings.Contains(errb.String(), "Error:") {
				t.Errorf("unexpected error: %q", errb.String())
			}
		})
	}
}
