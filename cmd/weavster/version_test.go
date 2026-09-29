package main

import (
	"bytes"
	"runtime"
	"strings"
	"testing"
)

// TestVersionCommand: `weavster version` prints the binary's version,
// build date, Go version, and platform without a server.
func TestVersionCommand(t *testing.T) {
	for _, tt := range []struct {
		args     []string
		code     int
		out, err string
	}{
		{[]string{"version"}, 0, "weavster " + version + " (built " + buildDate + ", " + runtime.Version() + ", " + runtime.GOOS + "/" + runtime.GOARCH + ")\n", ""},
		{[]string{"version", "-h"}, 0, "Usage: weavster version\n", ""},
		{[]string{"version", "extra"}, 2, "", `unexpected arguments ["extra"]`},
		{[]string{"version", "--json"}, 2, "", "flag provided but not defined: -json\nUsage: weavster version"},
		{[]string{"version", "-h", "extra"}, 0, "Usage: weavster version\n", ""},
	} {
		var out, errb bytes.Buffer
		if code := run(tt.args, strings.NewReader(""), &out, &errb); code != tt.code || out.String() != tt.out || !strings.Contains(errb.String(), tt.err) {
			t.Errorf("%v: exit %d %q %q", tt.args, code, out.String(), errb.String())
		}
	}
}
