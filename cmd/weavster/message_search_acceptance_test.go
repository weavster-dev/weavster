package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMessageSearch: messages are found by metadata, attempts, content
// type, and id range as well as flow and status; X-Total-Count gives every
// match while a page holds only limit of them; a bulk removal takes the
// same filters.
func TestMessageSearch(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer down.Close()
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\n"+storeConfig(t)+
		"delivery: {maxAttempts: 2, backoffBaseMs: 10, retryIntervalMs: 50}\n") // held: two attempts, then dead-lettered
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)

	in, out := t.TempDir(), t.TempDir()
	old := time.Now().Add(-time.Minute)
	for _, name := range []string{"a.json", "b.json"} {
		p := filepath.Join(in, name)
		if err := os.WriteFile(p, []byte(`{"k":"`+name+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	createFlow(t, c, `{"id":"files","source":{"type":"file","dir":"`+in+`","pattern":"*.json","pollIntervalMs":100},`+
		`"destinations":[{"name":"out","type":"file","dir":"`+out+`"}]}`)
	createFlow(t, c, `{"id":"held","destinations":[{"name":"ehr","type":"http","url":"`+down.URL+`"}]}`)
	heldID, _ := sendMessage(t, c, "held", `{"k":"held"}`)

	type found struct {
		ID          string
		ContentType string
	}
	search := func(query string) ([]found, string) {
		t.Helper()
		code, body, header := c.do(http.MethodGet, "/api/v1/messages?"+query, "", admin)
		var ms []found
		if code != http.StatusOK || json.Unmarshal([]byte(body), &ms) != nil {
			t.Fatalf("search %s: %d %s", query, code, body)
		}
		return ms, header.Get("X-Total-Count")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		sent, _ := search("flowId=files&status=sent")
		dead, _ := search("flowId=held&status=dead-lettered")
		if len(sent) == 2 && len(dead) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the two files and the dead-lettered message")
		}
		time.Sleep(20 * time.Millisecond)
	}
	all, total := search("sort=id")
	if len(all) != 3 || total != "3" {
		t.Fatalf("all messages: %d, X-Total-Count %q", len(all), total)
	}

	for _, tt := range []struct {
		name, query string
		want        int
		total       string
	}{
		{"metadata", "metadata.source.file=a.json", 1, "1"},
		{"metadata and flow", "flowId=files&metadata.source.file=b.json", 1, "1"},
		{"metadata without a match", "metadata.source.file=c.json", 0, "0"},
		{"one attempt", "minAttempts=1&maxAttempts=1", 2, "2"},
		{"two attempts", "minAttempts=2", 1, "1"},
		{"more attempts than any", "minAttempts=3", 0, "0"},
		{"content type", "contentType=" + all[0].ContentType, 3, "3"},
		{"id range", "idFrom=" + all[1].ID + "&idTo=" + all[2].ID, 2, "2"},
		{"a page of the total", "limit=1", 1, "3"},
		{"a page past the end", "offset=5", 0, "3"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ms, total := search(tt.query)
			if len(ms) != tt.want || total != tt.total {
				t.Errorf("%s: %d messages, X-Total-Count %q; want %d, %q", tt.query, len(ms), total, tt.want, tt.total)
			}
		})
	}
	if ms, _ := search("minAttempts=2"); len(ms) != 1 || ms[0].ID != heldID {
		t.Errorf("the message with two attempts = %+v, want %s", ms, heldID)
	}
	if code, body, _ := c.do(http.MethodGet, "/api/v1/messages?minAttempts=0", "", admin); code != http.StatusBadRequest || !strings.Contains(body, "minAttempts must be between 1 and 1000") {
		t.Errorf("minAttempts=0: %d %s", code, body)
	}

	// A bulk removal by metadata removes only the match.
	if code, body, _ := c.do(http.MethodDelete, "/api/v1/messages?metadata.source.file=a.json", "", admin); code != http.StatusOK || !strings.Contains(body, `"deleted":1`) {
		t.Fatalf("remove by metadata: %d %s", code, body)
	}
	if _, total := search(""); total != "2" {
		t.Errorf("after removing a.json: X-Total-Count %q, want 2", total)
	}
}
