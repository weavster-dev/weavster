package main

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestMessageArchive: an exported archive restores deleted messages, skips
// or overwrites existing ones, can move them to another flow, and can be
// encrypted; the CLI commands do the same through files.
func TestMessageArchive(t *testing.T) {
	c := startComposed(t, serverconfig.Default(), io.Discard)
	admin := basic(bootstrapAdmin, testAdminPassword)
	dir := t.TempDir()
	for _, id := range []string{"a", "b"} {
		createFlow(t, c, `{"id":"`+id+`","name":"Flow `+id+`","destinations":[{"name":"out","type":"file","dir":"`+dir+`"}]}`)
	}
	var ids []string
	for _, body := range []string{"one", "two", "three"} {
		id, _ := sendMessage(t, c, "a", body)
		ids = append(ids, id)
	}
	const key = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="
	withKey := func(k string) func(*http.Request) {
		return func(r *http.Request) {
			r.SetBasicAuth(bootstrapAdmin, testAdminPassword)
			r.Header.Set("Weavster-Archive-Key", k)
		}
	}
	code, archive, hdr := c.do(http.MethodGet, "/api/v1/messages/export?flowId=a", "", admin)
	if code != http.StatusOK || hdr.Get("Weavster-Message-Count") != "3" || hdr.Get("Content-Type") != "application/gzip" {
		t.Fatalf("export: %d, count %q, type %q", code, hdr.Get("Weavster-Message-Count"), hdr.Get("Content-Type"))
	}
	_, encrypted, _ := c.do(http.MethodGet, "/api/v1/messages/export?flowId=a", "", withKey(key))
	for _, id := range ids {
		if code, body, _ := c.do(http.MethodDelete, "/api/v1/messages/"+id, "", admin); code != http.StatusNoContent {
			t.Fatalf("delete %s: %d %q", id, code, body)
		}
	}
	steps := []struct {
		name, path, body string
		creds            func(*http.Request)
		status           int
		want             string
	}{
		{"restore", "/api/v1/messages/import", archive, admin, http.StatusOK, `{"imported":3,"skipped":0,"busy":0}`},
		{"existing ids skipped", "/api/v1/messages/import", archive, admin, http.StatusOK, `{"imported":0,"skipped":3,"busy":0}`},
		{"overwrite into flow b", "/api/v1/messages/import?flowId=b&overwrite=true", archive, admin, http.StatusOK, `{"imported":3,"skipped":0,"busy":0}`},
		{"unknown flowId", "/api/v1/messages/import?flowId=zz", archive, admin, http.StatusNotFound, "flowId zz"},
		{"encrypted without key", "/api/v1/messages/import", encrypted, admin, http.StatusBadRequest, "invalid message archive"},
		{"encrypted with wrong key", "/api/v1/messages/import", encrypted, withKey("HwAeAB0AHAAbABoAGQAYABcAFgAVABQAEwASABEAEAA="), http.StatusBadRequest, "invalid message archive"},
		{"encrypted with key", "/api/v1/messages/import?overwrite=true", encrypted, withKey(key), http.StatusOK, `{"imported":3,"skipped":0,"busy":0}`},
		{"not an archive", "/api/v1/messages/import", "hello", admin, http.StatusBadRequest, "invalid message archive"},
	}
	for _, s := range steps {
		code, body, _ := c.do(http.MethodPost, s.path, s.body, s.creds)
		if code != s.status || !strings.Contains(body, s.want) {
			t.Errorf("%s: %d %q; want %d containing %q", s.name, code, body, s.status, s.want)
		}
	}
	// Restored with status and content; the encrypted import put them back in a.
	if _, body, _ := c.do(http.MethodGet, "/api/v1/messages/"+ids[1], "", admin); !strings.Contains(body, `"flowId":"a","status":"sent"`) {
		t.Errorf("restored message = %s", body)
	}
	if _, body, _ := c.do(http.MethodGet, "/api/v1/messages/"+ids[1]+"/content", "", admin); body != "two" {
		t.Errorf("restored content = %q", body)
	}

	// An archive of a flow this server does not have is refused, unless
	// flowId assigns its messages to an existing flow.
	c.do(http.MethodPost, "/api/v1/flows/b/stop", "", admin)
	c.do(http.MethodPost, "/api/v1/messages/import?flowId=b&overwrite=true", archive, admin)
	_, bArchive, _ := c.do(http.MethodGet, "/api/v1/messages/export?flowId=b", "", admin)
	for _, id := range ids {
		c.do(http.MethodDelete, "/api/v1/messages/"+id, "", admin)
	}
	c.do(http.MethodPost, "/api/v1/flows/b/undeploy", "", admin)
	c.do(http.MethodDelete, "/api/v1/flows/b", "", admin)
	if code, body, _ := c.do(http.MethodPost, "/api/v1/messages/import", bArchive, admin); code != http.StatusNotFound || !strings.Contains(body, "flow b, which does not exist here") {
		t.Errorf("archive of a missing flow: %d %q, want 404", code, body)
	}
	if code, body, _ := c.do(http.MethodPost, "/api/v1/messages/import?flowId=a", bArchive, admin); code != http.StatusOK || !strings.Contains(body, `"imported":3`) {
		t.Errorf("archive of a missing flow into a: %d %q", code, body)
	}

	// CLI: export flow a by name to a file, then import it into flow a.
	file := filepath.Join(t.TempDir(), "flow a.json.gz")
	script := filepath.Join(t.TempDir(), "s.txt")
	for _, tt := range []struct {
		line, want string
		code       int
	}{
		{`exportmessages "` + file + `" "Flow a"`, "exported 3 messages to " + file, 0},
		{`importmessages "` + file + `" "Flow a"`, `{"imported":0,"skipped":3,"busy":0}`, 0},
		{`exportmessages "` + file + `"`, "", 2},
		{`exportmessages "` + file + `" a raw`, "", 2},
		{`importmessages "` + file + `" nope`, "", 2},
	} {
		_ = os.WriteFile(script, []byte(tt.line+"\n"), 0o600)
		var out, errb bytes.Buffer
		if code := run([]string{"-a", c.base, "-u", bootstrapAdmin, "-p", testAdminPassword, "-s", script}, strings.NewReader(""), &out, &errb); code != tt.code || !strings.Contains(out.String(), tt.want) {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", tt.line, code, out.String(), errb.String())
		}
	}
}
