package main

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestFileSourceRecursive: a recursive file source reads nested files in
// path order, keeps their relative paths (metadata and moveTo), and follows
// no symbolic link and no hidden directory; a relative file destination
// dir and a moveTo inside a recursive dir are refused.
func TestFileSourceRecursive(t *testing.T) {
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	in, done, out, elsewhere := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	old := time.Now().Add(-time.Minute)
	put := func(rel, body string) {
		t.Helper()
		p := filepath.Join(in, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_ = os.Chtimes(p, old, old)
	}
	put("top.json", `{"k":"top"}`)
	put("2026/09/a.json", `{"k":"a"}`)
	put("2026/10/b.json", `{"k":"b"}`)
	put(".cache/hidden.json", `{"k":"hidden"}`)
	if err := os.WriteFile(filepath.Join(elsewhere, "secret.json"), []byte(`{"k":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(in, "linked")); err != nil {
		t.Fatal(err)
	}
	createFlow(t, c, `{"id":"deep","source":{"type":"file","dir":"`+in+`","pattern":"*.json","recursive":true,"pollIntervalMs":100,"moveTo":"`+done+`"},`+
		`"destinations":[{"name":"out","type":"file","dir":"`+out+`"}]}`)

	tree := func(root string) string {
		var files []string
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				rel, _ := filepath.Rel(root, p)
				files = append(files, filepath.ToSlash(rel))
			}
			return nil
		})
		sort.Strings(files)
		return strings.Join(files, ",")
	}
	deadline := time.Now().Add(10 * time.Second)
	for tree(done) != "2026/09/a.json,2026/10/b.json,top.json" {
		if time.Now().After(deadline) {
			t.Fatalf("moved: %q", tree(done))
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := tree(in); got != ".cache/hidden.json,linked" {
		t.Errorf("left in the source: %q", got)
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "secret.json")); err != nil {
		t.Errorf("the linked directory's file was touched: %v", err)
	}
	_, body, _ := c.do(http.MethodGet, "/api/v1/messages?flowId=deep", "", admin)
	for _, want := range []string{`"source.file":"2026/09/a.json"`, `"source.file":"2026/10/b.json"`, `"source.file":"top.json"`} {
		if !strings.Contains(body, want) {
			t.Errorf("messages lack %s: %s", want, body)
		}
	}
	if strings.Contains(body, "secret") || strings.Contains(body, "hidden") {
		t.Errorf("a linked or hidden file was read: %s", body)
	}

	for flow, want := range map[string]string{
		`{"id":"x","source":{"type":"file","dir":"` + in + `/sub","recursive":true,"moveTo":"` + in + `/sub/done"}}`: "must not be inside source.dir when recursive",
		`{"id":"x","destinations":[{"name":"out","type":"file","dir":"relative/out"}]}`:                              "dir must be an absolute path",
		`{"id":"x","source":{"type":"file","dir":"` + in + `/2026"}}`:                                                "reads " + in + " recursively, which holds flow x's " + in + "/2026",
		`{"id":"x","source":{"type":"file","dir":"` + t.TempDir() + `","moveTo":"` + in + `/archive"}}`:              "which holds flow x's " + in + "/archive",
	} {
		if code, resp, _ := c.do(http.MethodPost, "/api/v1/flows", flow, admin); code != http.StatusBadRequest || !strings.Contains(resp, want) {
			t.Errorf("%s: %d %s", flow, code, resp)
		}
	}
}
