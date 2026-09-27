package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/weavster-dev/weavster/internal/gateway"
)

func TestGitAdapterCommit(t *testing.T) {
	dir := t.TempDir()
	a, err := newGitAdapter(dir)
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

	// A failed write puts back what the commit touched: the removed
	// settings file and the rewritten script.
	if err := os.MkdirAll(filepath.Join(dir, "scripts", "new.yaml"), 0o750); err != nil { // cannot be written as a file
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
		t.Errorf("rollback left changes: %v", changed)
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
