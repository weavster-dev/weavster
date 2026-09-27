package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/gitstore"
	"github.com/weavster-dev/weavster/internal/serverconfig"
)

var gitstoreAuthor = gitstore.Author{Name: "test"}

func TestGitAdapterCommit(t *testing.T) {
	dir := t.TempDir()
	a, err := newGitAdapter(serverconfig.Git{Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	script := func(v string) map[string]json.RawMessage {
		b, _ := json.Marshal(v)
		return map[string]json.RawMessage{"deploy": b}
	}
	first, err := a.GitCommit(ctx, gateway.ConfigBundle{Scripts: script("one"), Settings: map[string]json.RawMessage{"k": json.RawMessage(`1`)}}, "first", "admin")
	if err != nil || !first.Committed {
		t.Fatalf("first = %+v %v", first, err)
	}
	read := func(f string) string {
		b, _ := os.ReadFile(filepath.Join(dir, f))
		return string(b)
	}

	// A file that cannot be backed up (here a directory where a script
	// file goes) aborts the commit before anything changes.
	if err := os.MkdirAll(filepath.Join(dir, "scripts", "new.yaml"), 0o750); err != nil {
		t.Fatal(err)
	}
	newScript := map[string]json.RawMessage{"deploy": json.RawMessage(`"two"`), "new": json.RawMessage(`"x"`)}
	if _, err := a.GitCommit(ctx, gateway.ConfigBundle{Scripts: newScript}, "fails", "admin"); err == nil {
		t.Fatal("commit over a directory succeeded")
	}
	if got := read("settings/k.yaml"); got != "version: \"1\"\nsettings:\n    k: 1\n" {
		t.Errorf("settings not restored: %q", got)
	}
	if got := read("scripts/deploy.yaml"); got != "version: \"1\"\nscripts:\n    deploy: one\n" {
		t.Errorf("script not restored: %q", got)
	}
	if err := os.RemoveAll(filepath.Join(dir, "scripts", "new.yaml")); err != nil {
		t.Fatal(err)
	}
	if changed, _ := a.store.WorkingTreeDiff(); len(changed) != 0 {
		t.Errorf("abort left changes: %v", changed)
	}

	// A failed write puts back what the commit touched: the removed
	// settings file, and the new script file is removed again.
	if os.Geteuid() != 0 { // root writes read-only files
		deploy := filepath.Join(dir, "scripts", "deploy.yaml")
		if err := os.Chmod(deploy, 0o400); err != nil {
			t.Fatal(err)
		}
		if _, err := a.GitCommit(ctx, gateway.ConfigBundle{Scripts: newScript}, "fails", "admin"); err == nil {
			t.Fatal("commit over a read-only file succeeded")
		}
		if err := os.Chmod(deploy, 0o600); err != nil {
			t.Fatal(err)
		}
		if got := read("settings/k.yaml"); got != "version: \"1\"\nsettings:\n    k: 1\n" {
			t.Errorf("settings not restored: %q", got)
		}
		if changed, _ := a.store.WorkingTreeDiff(); len(changed) != 0 {
			t.Errorf("rollback left changes: %v", changed)
		}
	}

	// Files below a section directory are not managed.
	if err := os.MkdirAll(filepath.Join(dir, "flows", "examples"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "flows", "examples", "demo.yaml"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := a.GitCommit(ctx, gateway.ConfigBundle{Scripts: script("one")}, "drop settings", "admin")
	if err != nil || len(res.Changed) != 2 || res.Changed[0] != "flows/examples/demo.yaml" || res.Changed[1] != "settings/k.yaml" {
		t.Errorf("commit = %+v %v", res, err)
	}
	if read("flows/examples/demo.yaml") != "x" {
		t.Error("unmanaged file removed")
	}

	// Names differing only in case are refused.
	clash := gateway.ConfigBundle{Scripts: map[string]json.RawMessage{"Deploy": json.RawMessage(`"a"`), "deploy": json.RawMessage(`"b"`)}}
	if _, err := a.GitCommit(ctx, clash, "clash", "admin"); !errors.Is(err, gateway.ErrGitNameClash) {
		t.Errorf("clash = %v", err)
	}
}

func TestManagedFile(t *testing.T) {
	for f, want := range map[string]bool{
		"flows/adt.yaml": true, "snippetLibraries/hl7.yaml": true, "settings/k.yaml": true,
		"flows/examples/demo.yaml": false, "flows/adt.yml": false, "configmap/region.yaml": false,
		"README.md": false, "docs/a.yaml": false,
	} {
		if got := managedFile(f); got != want {
			t.Errorf("%s = %v, want %v", f, got, want)
		}
	}
}

func TestGitAdapterDocument(t *testing.T) {
	a, err := newGitAdapter(serverconfig.Git{Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, _, err := a.GitDocument(ctx, "HEAD"); !errors.Is(err, gateway.ErrGitNotFound) {
		t.Errorf("empty repository = %v", err)
	}
	for _, tt := range []struct {
		name, file, content, want string
	}{
		{"several artifacts in one file", "flows/many.yaml", "version: \"1\"\nscripts:\n  a: x\n  b: \"1\"\nsettings:\n  n: 9007199254740993\n",
			"version: \"1\"\nflows: {}\nalerts: {}\nsnippets: {}\nsnippetLibraries: {}\nscripts:\n    a: x\n    b: \"1\"\nsettings:\n    n: 9007199254740993\n"},
		{"unmanaged files ignored", "flows/examples/x.yaml", "not yaml: [", "scripts:\n    a: x"},
		{"bad file", "alerts/bad.yaml", "version: \"1\"\nalerts:\n  x: [", "alerts/bad.yaml"},
		{"unknown field", "settings/bad.yaml", "version: \"1\"\nbogus: 1\n", "settings/bad.yaml"},
		{"config map refused", "settings/map.yaml", "version: \"1\"\nconfigmap:\n  region: eu\n", "the config map is not read from the repository"},
		{"schema error names the file", "flows/bad.yaml", "version: \"1\"\nflows:\n  bad:\n    id: bad\n    destinations: [{name: out, type: nope}]\n", "flows/bad.yaml: "},
		{"broken reference names the file", "snippets/pid.yaml", "version: \"1\"\nsnippets:\n  pid: {name: pid, library: none, code: x}\n", "snippets/pid.yaml: "},
		{"aliases refused", "scripts/alias.yaml", "version: \"1\"\nscripts:\n  b: &x foo\n  c: *x\n", "scripts/alias.yaml: YAML anchors and aliases are not supported"},
		{"merge keys refused", "settings/merge.yaml", "version: \"1\"\nsettings:\n  m:\n    <<: {a: 1}\n", "settings/merge.yaml: YAML anchors and aliases"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := a.store.WriteFile(tt.file, []byte(tt.content)); err != nil {
				t.Fatal(err)
			}
			if _, err := a.store.Commit(tt.name, gitstoreAuthor); err != nil {
				t.Fatal(err)
			}
			doc, commit, err := a.GitDocument(ctx, "HEAD")
			got := string(doc)
			if head, _, _ := a.store.Head(); err == nil && commit != head {
				t.Errorf("commit = %s, want %s", commit, head)
			}
			if err != nil {
				got = err.Error()
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if err != nil { // leave the repository valid for the next case
				if err := a.store.RemoveFile(tt.file); err != nil {
					t.Fatal(err)
				}
				if _, err := a.store.Commit("undo", gitstoreAuthor); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// TestGitAdapterDocumentNamesOnlyTheBadFile: a problem with flows.adt
// names adt's file, not the file of flows.ad.
func TestGitAdapterDocumentNamesOnlyTheBadFile(t *testing.T) {
	a, err := newGitAdapter(serverconfig.Git{Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for f, content := range map[string]string{
		"flows/ad.yaml":  "version: \"1\"\nflows:\n  ad: {id: ad}\n",
		"flows/adt.yaml": "version: \"1\"\nflows:\n  adt:\n    id: adt\n    destinations: [{name: out, type: nope}]\n",
	} {
		if err := a.store.WriteFile(f, []byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.store.Commit("two flows", gitstoreAuthor); err != nil {
		t.Fatal(err)
	}
	_, _, err = a.GitDocument(context.Background(), "HEAD")
	if err == nil || !strings.Contains(err.Error(), "flows/adt.yaml: ") || strings.Contains(err.Error(), "flows/ad.yaml") {
		t.Errorf("err = %v", err)
	}
}

func TestRemoteError(t *testing.T) {
	r := gitstore.Remote{URL: "https://example.com/r.git", Password: "s3cret"}
	for _, tt := range []struct {
		err    error
		is     error
		want   string
		absent string
	}{
		{gitstore.ErrRejected, gateway.ErrGitConflict, "pull first", ""},
		{gitstore.ErrNothingToPush, gateway.ErrGitConflict, "no commits", ""},
		{gitstore.ErrNotFound, gateway.ErrGitConflict, "the remote has no branch main; push first", ""},
		{errors.New("auth failed for s3cret"), gateway.ErrGitRemote, "https://example.com/r.git: auth failed for ***", "s3cret"},
	} {
		got := remoteError(r, "main", tt.err)
		if !errors.Is(got, tt.is) || !strings.Contains(got.Error(), tt.want) || (tt.absent != "" && strings.Contains(got.Error(), tt.absent)) {
			t.Errorf("%v -> %v", tt.err, got)
		}
	}
	if _, err := (gitAdapter{}).remoteOf(); !errors.Is(err, gateway.ErrGitConflict) {
		t.Errorf("no remote = %v", err)
	}
	t.Setenv("WEAVSTER_TEST_TOKEN", "tok")
	if r, err := (gitAdapter{remote: serverconfig.GitRemote{URL: "u", Username: "ci", PasswordEnv: "WEAVSTER_TEST_TOKEN"}}).remoteOf(); err != nil || r.Password != "tok" || r.Username != "ci" {
		t.Errorf("remote = %+v %v", r, err)
	}
}
