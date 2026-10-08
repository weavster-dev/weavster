package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestMessagesAPI: search filters and paging run in the store (a flow
// filter with a small limit still finds that flow's messages), and a
// message can be read, its content fetched, reprocessed, and removed.
func TestMessagesAPI(t *testing.T) {
	c := startComposed(t, serverconfig.Default(), io.Discard)
	admin := basic(bootstrapAdmin, testAdminPassword)
	dir := t.TempDir()
	for _, id := range []string{"a", "b"} {
		createFlow(t, c, `{"id":"`+id+`","destinations":[{"name":"out","type":"file","dir":"`+dir+`"}]}`)
	}
	var aIDs []string
	for i, flow := range []string{"a", "b", "b", "b", "a"} {
		id, status := sendMessage(t, c, flow, "msg-"+string(rune('0'+i)))
		if status != "sent" {
			t.Fatalf("send: %s", status)
		}
		if flow == "a" {
			aIDs = append(aIDs, id)
		}
		time.Sleep(2 * time.Millisecond) // distinct receive times
	}
	list := func(query string) []struct{ ID, FlowID string } {
		t.Helper()
		code, body, _ := c.do(http.MethodGet, "/api/v1/messages"+query, "", admin)
		if code != http.StatusOK {
			t.Fatalf("search %s: %d %q", query, code, body)
		}
		var out []struct{ ID, FlowID string }
		_ = json.Unmarshal([]byte(body), &out)
		return out
	}
	// Flow filter before the limit: the two a-messages, newest first.
	if got := list("?flowId=a&limit=2"); len(got) != 2 || got[0].ID != aIDs[1] || got[1].ID != aIDs[0] {
		t.Errorf("flowId=a&limit=2 = %+v, want %v newest first", got, aIDs)
	}
	if got := list("?flowId=b&limit=2&offset=2"); len(got) != 1 || got[0].FlowID != "b" {
		t.Errorf("flowId=b page 2 = %+v, want the third b-message", got)
	}
	if got := list("?sort=receivedAt&limit=1"); len(got) != 1 || got[0].ID != aIDs[0] {
		t.Errorf("oldest first = %+v", got)
	}
	if got := list("?from=2999-01-01T00:00:00Z"); len(got) != 0 {
		t.Errorf("future from = %+v", got)
	}

	first := aIDs[0]
	steps := []struct {
		method, path string
		status       int
		contains     string
	}{
		{http.MethodGet, "/api/v1/messages/" + first, http.StatusOK, `"flowId":"a","status":"sent"`},
		{http.MethodGet, "/api/v1/messages/" + first, http.StatusOK, `"attempts":{"out":{"attempts":1,"lastAttemptAt":"`},
		{http.MethodGet, "/api/v1/messages/" + first + "/content", http.StatusOK, "msg-0"},
		{http.MethodGet, "/api/v1/messages/" + first + "/content?part=transformed", http.StatusOK, "msg-0"},
		{http.MethodPost, "/api/v1/messages/" + first + "/reprocess", http.StatusAccepted, `"status":"sent"`},
		{http.MethodGet, "/api/v1/messages/" + first + "/content?part=encoded", http.StatusBadRequest, "raw or transformed"},
		{http.MethodDelete, "/api/v1/messages/" + first, http.StatusNoContent, ""},
		{http.MethodGet, "/api/v1/messages/" + first, http.StatusNotFound, "message not found"},
		{http.MethodDelete, "/api/v1/messages/" + first, http.StatusNotFound, "message not found"},
		{http.MethodPost, "/api/v1/messages/" + first + "/reprocess", http.StatusNotFound, "message not found"},
		{http.MethodGet, "/api/v1/messages?limit=0", http.StatusBadRequest, "between 1 and 1000"},
	}
	var reprocessed string
	for _, s := range steps {
		code, body, _ := c.do(s.method, s.path, "", admin)
		if code != s.status || !strings.Contains(body, s.contains) {
			t.Errorf("%s %s: %d %q; want %d containing %q", s.method, s.path, code, body, s.status, s.contains)
		}
		if strings.HasSuffix(s.path, "/reprocess") && code == http.StatusAccepted {
			var r struct{ ID string }
			_ = json.Unmarshal([]byte(body), &r)
			reprocessed = r.ID
		}
	}
	if _, body, _ := c.do(http.MethodGet, "/api/v1/messages/"+reprocessed, "", admin); !strings.Contains(body, `"reprocessedFrom":"`+first+`"`) {
		t.Errorf("reprocessed message = %s, want reprocessedFrom %s", body, first)
	}
	// Reprocessing needs the flow started.
	c.do(http.MethodPost, "/api/v1/flows/a/stop", "", admin)
	if code, body, _ := c.do(http.MethodPost, "/api/v1/messages/"+reprocessed+"/reprocess", "", admin); code != http.StatusConflict {
		t.Errorf("reprocess into a stopped flow: %d %q, want 409", code, body)
	}
}
