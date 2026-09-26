package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestShell covers the interactive shell and the startup flags against a
// running server: a session over stdin, the connection file (-c) and its
// overrides, login failure returning to the prompt, -v, and -d.
func TestShell(t *testing.T) {
	handler, closeStore, err := buildServer(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), io.Discard, serverconfig.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = closeStore() }()
	ts := httptest.NewServer(handler)
	defer ts.Close()
	c := apiClient{t: t, base: ts.URL}
	admin := basic(bootstrapAdmin, testAdminPassword)
	for _, id := range []string{"adt", "orm"} {
		if code, body, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"`+id+`","name":"`+strings.ToUpper(id)+`"}`, admin); code != http.StatusCreated {
			t.Fatalf("create %s: %d %q", id, code, body)
		}
	}
	dir := t.TempDir()
	file := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := file("good.yaml", "address: "+ts.URL+"\nuser: "+bootstrapAdmin+"\npassword: "+testAdminPassword+"\n")
	wrongPass := file("wrong.yaml", "address: "+ts.URL+"\nuser: "+bootstrapAdmin+"\npassword: nope\n")
	unknownKey := file("unknown.yaml", "address: "+ts.URL+"\nhost: x\n")

	tests := []struct {
		name           string
		args           []string
		stdin          string
		code           int
		stdout, stderr []string
	}{
		{"interactive session", []string{"-c", good}, "status\nflow deploy adt\nstatus\nbogus\nflow list\nquit\nflow list\n", 0,
			[]string{shellPrompt + "no deployed flows", `"status":"deployed"`, shellPrompt + "adt\tdeployed\tADT", "orm\tundeployed\tORM"},
			[]string{`unknown command "bogus"`}},
		{"end of input ends the shell", []string{"-c", good}, "help\n", 0, []string{"commands:"}, nil},
		{"login failure returns to the prompt", []string{"-c", wrongPass}, "flow list\nquit\n", 0,
			[]string{shellPrompt}, []string{"Could not log in to server.", "401 Unauthorized"}},
		{"flags override the file", []string{"-c", wrongPass, "-p", testAdminPassword}, "flow list\n", 0,
			[]string{"adt\tdeployed\tADT"}, nil},
		{"login failure in batch mode", []string{"-a", ts.URL, "-u", bootstrapAdmin, "-p", "nope", "-s", file("s.txt", "flow list\n")}, "", 2,
			nil, []string{"Could not log in to server.", "401 Unauthorized"}},
		{"missing connection file", []string{"-c", filepath.Join(dir, "none.yaml")}, "", 2, nil, []string{"Error: connection file"}},
		{"unknown key in connection file", []string{"-c", unknownKey}, "", 2, nil, []string{"field host not found"}},
		{"server version", []string{"-c", good, "-v"}, "", 0, []string{"weavster server " + version}, nil},
		{"server version needs credentials", []string{"-a", ts.URL, "-v"}, "", 2, nil, []string{"401 Unauthorized"}},
		{"server version with a bad login", []string{"-c", wrongPass, "-v"}, "", 2, nil, []string{"Could not log in to server.", "401 Unauthorized"}},
		{"password without a user", []string{"-a", ts.URL, "-p", "secret"}, "", 2, nil, []string{"a password needs a user"}},
		{"quit with extra words", []string{"-c", good}, "quit now\nflow list\n", 0, []string{shellPrompt}, nil},
		{"line too long", []string{"-c", good}, strings.Repeat("x", maxShellLine+1) + "\n", 2, nil, []string{"reading commands"}},
		{"debug shows causes", []string{"-a", "http://127.0.0.1:1", "-d", "-s", file("d.txt", "flow list\n")}, "", 2,
			nil, []string{"Error: Get", "caused by"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			code := run(tt.args, strings.NewReader(tt.stdin), &out, &errb)
			if code != tt.code {
				t.Errorf("exit = %d, want %d (stderr %q)", code, tt.code, errb.String())
			}
			if tt.name == "quit with extra words" && strings.Contains(out.String(), "adt\t") {
				t.Errorf("commands after quit ran: %q", out.String())
			}
			for _, want := range tt.stdout {
				if !strings.Contains(out.String(), want) {
					t.Errorf("stdout %q does not contain %q", out.String(), want)
				}
			}
			for _, want := range tt.stderr {
				if !strings.Contains(errb.String(), want) {
					t.Errorf("stderr %q does not contain %q", errb.String(), want)
				}
			}
		})
	}
}
