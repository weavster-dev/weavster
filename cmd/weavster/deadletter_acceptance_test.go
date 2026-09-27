package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDeadLetterRequeue: a message whose HTTP destination stays down is
// dead-lettered; the CLI lists and shows it, requeue delivers it to the
// failed destination only once the destination is back (the file
// destination that already had it is not written again), the previous
// attempts stay in a message.requeued event, and dead letters can be
// requeued in bulk or removed.
func TestDeadLetterRequeue(t *testing.T) {
	url, up, hits := flakyDownstream(t)
	addr, archive := freeAddr(t), t.TempDir()
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n"+
		"delivery: {maxAttempts: 2, backoffBaseMs: 10, retryIntervalMs: 20}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	createFlow(t, c, `{"id":"f","destinations":[{"name":"archive","type":"file","dir":"`+archive+`"},{"name":"ehr","type":"http","url":"`+url+`"}]}`)
	cli := func(line string) (int, string, string) {
		t.Helper()
		script := filepath.Join(t.TempDir(), "s.txt")
		if err := os.WriteFile(script, []byte(line+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var out, errb bytes.Buffer
		code := run([]string{"-a", c.base, "-u", bootstrapAdmin, "-p", testAdminPassword, "-s", script}, strings.NewReader(""), &out, &errb)
		return code, out.String(), errb.String()
	}
	archived := func() int {
		entries, _ := os.ReadDir(archive)
		return len(entries)
	}

	id, _ := sendMessage(t, c, "f", "x")
	waitStatus(t, c, id, "dead-lettered")
	if code, out, errOut := cli("deadletter list f"); code != 0 || !strings.Contains(out, id+"\tf\t") ||
		!strings.Contains(out, "archive: delivered; ehr: 2 attempts,") || !strings.Contains(out, "1 dead-lettered messages") {
		t.Errorf("list: %d %q %q", code, out, errOut)
	}
	if code, out, _ := cli("deadletter show " + id); code != 0 || !strings.Contains(out, `"status": "dead-lettered"`) {
		t.Errorf("show: %d %q", code, out)
	}

	// Requeue once the destination is back: only ehr is delivered again.
	up.Store(true)
	if code, out, errOut := cli("deadletter requeue " + id); code != 0 || !strings.Contains(out, "requeued "+id+" (before: archive: delivered; ehr: 2 attempts,") {
		t.Fatalf("requeue: %d %q %q", code, out, errOut)
	}
	waitStatus(t, c, id, "sent")
	if hits.Load() != 1 || archived() != 1 {
		t.Errorf("deliveries: ehr %d, archive files %d; want 1 and 1", hits.Load(), archived())
	}
	_, body, _ := c.do(http.MethodGet, "/api/v1/messages/"+id, "", admin)
	var msg struct {
		Metadata map[string]string
		Attempts map[string]struct{ Attempts int }
	}
	_ = json.Unmarshal([]byte(body), &msg)
	if msg.Metadata["requeues"] != "1" || msg.Attempts["ehr"].Attempts != 1 || msg.Attempts["archive"].Attempts != 1 {
		t.Errorf("message after requeue = %s", body)
	}
	if _, body, _ := c.do(http.MethodGet, "/api/v1/events?type=message.requeued", "", admin); !strings.Contains(body, `"previous.ehr.attempts":"2"`) || !strings.Contains(body, `"messageId":"`+id+`"`) {
		t.Errorf("requeue event = %s", body)
	}
	// A sent message cannot be requeued.
	if code, body, _ := c.do(http.MethodPost, "/api/v1/messages/"+id+"/requeue", "", admin); code != http.StatusConflict || !strings.Contains(body, "not dead-lettered (status sent)") {
		t.Errorf("requeue sent: %d %s", code, body)
	}

	// Bulk requeue and remove.
	up.Store(false)
	a, _ := sendMessage(t, c, "f", "a")
	b, _ := sendMessage(t, c, "f", "b")
	waitStatus(t, c, a, "dead-lettered")
	waitStatus(t, c, b, "dead-lettered")
	if code, _, errOut := cli("deadletter remove " + id); code != 2 || !strings.Contains(errOut, "is not dead-lettered (status sent)") {
		t.Errorf("remove sent: %d %q", code, errOut)
	}
	if code, out, _ := cli("deadletter remove " + b); code != 0 || out != "removed "+b+"\n" {
		t.Errorf("remove: %d %q", code, out)
	}
	up.Store(true)
	if code, out, _ := cli("deadletter requeue all f"); code != 0 || out != "requeued 1 messages, skipped 0\n" {
		t.Errorf("requeue all: %d %q", code, out)
	}
	waitStatus(t, c, a, "sent")
	for _, tt := range []struct{ line, want string }{
		{"deadletter requeue nope", "404"},
		{"deadletter requeue all nope", "404"},
		{"deadletter", "usage: deadletter"},
		{"deadletter frob", "usage: deadletter"},
	} {
		if code, _, errOut := cli(tt.line); code != 2 || !strings.Contains(errOut, tt.want) {
			t.Errorf("%s: %d %q", tt.line, code, errOut)
		}
	}
	// Requeue needs messages:send.
	c.do(http.MethodPost, "/api/v1/users", `{"username":"viewer","password":"View-Passw0rd-1","permissions":["messages:view"],"mustChangePassword":false}`, admin)
	if code, body, _ := c.do(http.MethodPost, "/api/v1/messages/requeue", "", basic("viewer", "View-Passw0rd-1")); code != http.StatusForbidden || !strings.Contains(body, "messages:send") {
		t.Errorf("viewer requeue: %d %s", code, body)
	}
}
