package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestFileSource: a started flow with a file source picks up the files in
// its directory (pattern-matched regular files that have settled), sends
// them through the flow, and moves or deletes them; stopping the flow stops
// the polling; files the flow rejects are moved aside or left and skipped.
func TestFileSource(t *testing.T) {
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n"+
		"delivery: {retryIntervalMs: 50}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	in, done, out := t.TempDir(), t.TempDir(), t.TempDir()
	old := time.Now().Add(-time.Minute)
	put := func(dir, name, body string, settled bool) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if settled {
			if err := os.Chtimes(p, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	names := func(dir string) string {
		entries, _ := os.ReadDir(dir)
		var n []string
		for _, e := range entries {
			n = append(n, e.Name())
		}
		sort.Strings(n)
		return strings.Join(n, ",")
	}
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	// Files waiting before the flow starts, plus ones that must be skipped.
	put(in, "a.json", `{"k":"a"}`, true)
	put(in, "b.json", `{"k":"b"}`, true)
	put(in, "notes.txt", "not matched", true)
	if err := os.Mkdir(filepath.Join(in, "sub.json"), 0o750); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.json")
	put(filepath.Dir(outside), "secret.json", `{"secret":true}`, true)
	if err := os.Symlink(outside, filepath.Join(in, "link.json")); err != nil {
		t.Fatal(err)
	}
	createFlow(t, c, `{"id":"adt","source":{"type":"file","dir":"`+in+`","pattern":"*.json","pollIntervalMs":100,"moveTo":"`+done+`"},`+
		`"destinations":[{"name":"out","type":"file","dir":"`+out+`"}]}`)
	waitFor("a.json and b.json to be processed", func() bool { return names(done) == "a.json,b.json" })
	if got := names(in); got != "link.json,notes.txt,sub.json" {
		t.Errorf("left in the source directory: %s", got)
	}
	waitFor("two deliveries", func() bool { return len(strings.Split(names(out), ",")) == 2 })
	_, body, _ := c.do(http.MethodGet, "/api/v1/messages?flowId=adt", "", admin)
	if !strings.Contains(body, `"source.file":"a.json"`) || !strings.Contains(body, `"source.file":"b.json"`) || strings.Contains(body, "secret") {
		t.Errorf("messages = %s", body)
	}

	// A file still being written (just modified) waits until it settles.
	put(in, "fresh.json", `{"k":"fresh"}`, false)
	time.Sleep(400 * time.Millisecond)
	if strings.Contains(names(done), "fresh.json") {
		t.Error("a file modified just now was read before it settled")
	}
	waitFor("fresh.json to be processed once settled", func() bool { return strings.Contains(names(done), "fresh.json") })

	// A stopped flow does not poll; starting it again resumes.
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows/adt/stop", "", admin); code != http.StatusOK {
		t.Fatalf("stop: %d %s", code, body)
	}
	put(in, "c.json", `{"k":"c"}`, true)
	time.Sleep(500 * time.Millisecond)
	if !strings.Contains(names(in), "c.json") {
		t.Error("a stopped flow read a file")
	}
	c.do(http.MethodPost, "/api/v1/flows/adt/start", "", admin)
	waitFor("c.json after start", func() bool { return strings.Contains(names(done), "c.json") })

	// Rejected files: moved to moveTo/rejected, or (without moveTo) left
	// in place, skipped until they change, and deleted when processed.
	in2, done2, in3 := t.TempDir(), t.TempDir(), t.TempDir()
	transform := `"transform":{"name":"t","steps":[{"map":{"from":"n","to":"n","type":"number"}}]}`
	createFlow(t, c, `{"id":"conv","source":{"type":"file","dir":"`+in2+`","pollIntervalMs":100,"moveTo":"`+done2+`"},`+transform+`}`)
	createFlow(t, c, `{"id":"keep","source":{"type":"file","dir":"`+in3+`","pollIntervalMs":100},`+transform+`}`)
	put(in2, "bad.txt", "not json", true)
	put(in3, "bad.txt", "not json", true)
	put(in3, "good.json", `{"n":"1"}`, true)
	put(in2, "huge.json", strings.Repeat(" ", 10<<20+1), true) // over the 10 MiB message limit
	waitFor("bad.txt and huge.json in moveTo/rejected", func() bool { return names(filepath.Join(done2, "rejected")) == "bad.txt,huge.json" })
	waitFor("good.json processed and deleted", func() bool { return names(in3) == "bad.txt" })
	time.Sleep(400 * time.Millisecond) // several more polls
	_, body, _ = c.do(http.MethodGet, "/api/v1/events?type=source.file.rejected", "", admin)
	var events []struct {
		FlowID string
		Data   map[string]string
	}
	_ = json.Unmarshal([]byte(body), &events)
	perFlow := map[string]int{}
	for _, e := range events {
		perFlow[e.FlowID]++
		switch e.Data["file"] {
		case "bad.txt":
			if !strings.Contains(e.Data["reason"], "JSON object") {
				t.Errorf("event = %+v", e)
			}
		case "huge.json":
			if e.Data["reason"] != "larger than 10 MiB" {
				t.Errorf("event = %+v", e)
			}
		default:
			t.Errorf("event = %+v", e)
		}
	}
	if perFlow["conv"] != 2 || perFlow["keep"] != 1 {
		t.Errorf("rejected events per flow = %v (a left file must not be retried)", perFlow)
	}

	// Relative directories are refused.
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"rel","source":{"type":"file","dir":"in"}}`, admin); code != http.StatusBadRequest || !strings.Contains(body, "source.dir must be an absolute path") {
		t.Errorf("relative dir: %d %s", code, body)
	}
}
