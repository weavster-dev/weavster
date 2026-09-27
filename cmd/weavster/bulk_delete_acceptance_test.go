package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestBulkMessageRemoval: messages are removed by flow, by status, or all
// at once (running flows are stopped and started again), removal without a
// filter needs all=true, and the CLI clears messages and dumps statistics
// and events to files.
func TestBulkMessageRemoval(t *testing.T) {
	c := startComposed(t, serverconfig.Default(), io.Discard)
	admin := basic(bootstrapAdmin, testAdminPassword)
	dir := t.TempDir()
	for _, id := range []string{"a", "b"} {
		createFlow(t, c, `{"id":"`+id+`","destinations":[{"name":"out","type":"file","dir":"`+dir+`"}]}`)
	}
	for _, flow := range []string{"a", "a", "b", "b", "b"} {
		sendMessage(t, c, flow, "msg")
	}
	// Flow e's transform fails on this message, so it is errored.
	createFlow(t, c, `{"id":"e","transform":{"name":"t","steps":[{"map":{"from":"name","to":"n","type":"number"}}]}}`)
	if _, status := sendMessage(t, c, "e", `{"name":"John Smith"}`); status != "errored" {
		t.Fatalf("errored fixture: status %s", status)
	}
	count := func(query string) int {
		t.Helper()
		_, body, _ := c.do(http.MethodGet, "/api/v1/messages"+query, "", admin)
		var out []json.RawMessage
		_ = json.Unmarshal([]byte(body), &out)
		return len(out)
	}
	steps := []struct {
		name, path string
		status     int
		want       string
		left       int
	}{
		{"no filter", "/api/v1/messages", http.StatusBadRequest, "all=true", 6},
		{"by status: only the errored message", "/api/v1/messages?status=errored", http.StatusOK, `{"deleted":1,"busy":0,"restarted":[]}`, 5},
		{"by status matching nothing", "/api/v1/messages?status=errored", http.StatusOK, `{"deleted":0,"busy":0,"restarted":[]}`, 5},
		{"by flow", "/api/v1/messages?flowId=a", http.StatusOK, `"deleted":2`, 3},
		{"clear all, restarting", "/api/v1/messages?all=true&restart=true", http.StatusOK, `"restarted":["a","b","e"]`, 0},
	}
	for _, s := range steps {
		code, body, _ := c.do(http.MethodDelete, s.path, "", admin)
		if code != s.status || !strings.Contains(body, s.want) {
			t.Errorf("%s: %d %q; want %d containing %q", s.name, code, body, s.status, s.want)
		}
		if n := count(""); n != s.left {
			t.Errorf("%s: %d messages left, want %d", s.name, n, s.left)
		}
	}
	// The flows were started again: they still accept messages.
	if _, status := sendMessage(t, c, "a", "after"); status != "sent" {
		t.Errorf("after restart: status %s", status)
	}

	// Permission: messages:delete.
	c.do(http.MethodPost, "/api/v1/users", `{"username":"viewer","password":"Viewer-Passw0rd","permissions":["messages:view"],"mustChangePassword":false}`, admin)
	if code, body, _ := c.do(http.MethodDelete, "/api/v1/messages?all=true", "", basic("viewer", "Viewer-Passw0rd")); code != http.StatusForbidden || !strings.Contains(body, "messages:delete") {
		t.Errorf("viewer: %d %q", code, body)
	}

	// CLI.
	stats, events, script := filepath.Join(dir, "stats.json"), filepath.Join(dir, "events.json"), filepath.Join(dir, "s.txt")
	for _, tt := range []struct {
		line, want string
		code       int
	}{
		{"clearallmessages", "removed 1 messages; restarted a, b, e", 0},
		{`dump stats "` + stats + `"`, "wrote stats to " + stats, 0},
		{`dump events "` + events + `"`, "wrote events to " + events, 0},
		{"clearallmessages now", "", 2},
		{`dump logs "x"`, "", 2},
		{`dump stats "` + filepath.Join(dir, "missing", "x.json") + `"`, "", 2},
	} {
		_ = os.WriteFile(script, []byte(tt.line+"\n"), 0o600)
		var out, errb bytes.Buffer
		if code := run([]string{"-a", c.base, "-u", bootstrapAdmin, "-p", testAdminPassword, "-s", script}, strings.NewReader(""), &out, &errb); code != tt.code || !strings.Contains(out.String(), tt.want) {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", tt.line, code, out.String(), errb.String())
		}
	}
	if b, _ := os.ReadFile(stats); !strings.Contains(string(b), `"a":{"received":`) {
		t.Errorf("stats file = %s", b)
	}
	if b, _ := os.ReadFile(events); !strings.Contains(string(b), `"type":`) {
		t.Errorf("events file = %s", b)
	}
	// The CLI surfaces a server error.
	_ = os.WriteFile(script, []byte("clearallmessages\n"), 0o600)
	var out, errb bytes.Buffer
	if code := run([]string{"-a", c.base, "-u", "viewer", "-p", "Viewer-Passw0rd", "-s", script}, strings.NewReader(""), &out, &errb); code != 2 || !strings.Contains(errb.String(), "messages:delete") {
		t.Errorf("viewer clearallmessages: exit %d, stderr %q", code, errb.String())
	}
}
