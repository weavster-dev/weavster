package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/state"
)

// TestPruneMessages: a server with prune limits removes, on demand, the
// messages older than maxAgeHours and then the oldest past maxMessages;
// the status shows the pass, and the events record it.
func TestPruneMessages(t *testing.T) {
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\n"+storeConfig(t)+
		"prune: {maxAgeHours: 24, maxMessages: 3, intervalMinutes: 60}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	createFlow(t, c, `{"id":"adt","destinations":[{"name":"out","type":"file","dir":"`+t.TempDir()+`"}]}`)

	// Two messages received three days ago (an archive keeps receive times).
	old := state.NewMemStore()
	for _, id := range []string{"old-1", "old-2"} {
		_ = old.Put(context.Background(), state.Message{ID: id, FlowID: "adt", Status: state.StatusSent, ReceivedAt: time.Now().Add(-72 * time.Hour), Raw: []byte("{}")})
	}
	archive, _, err := state.ExportArchive(context.Background(), old, state.ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if code, body, _ := c.do(http.MethodPost, "/api/v1/messages/import", string(archive), admin); code != http.StatusOK {
		t.Fatalf("import: %d %s", code, body)
	}
	var sent []string
	for range 4 {
		id, _ := sendMessage(t, c, "adt", `{"k":"v"}`)
		sent = append(sent, id)
		time.Sleep(5 * time.Millisecond) // distinct receive times: sent[0] is the oldest
	}

	if code, body, _ := c.do(http.MethodPost, "/api/v1/system/prune/stop", "", admin); code != http.StatusConflict {
		t.Errorf("stop with nothing running: %d %s", code, body)
	}
	if code, body, _ := c.do(http.MethodPost, "/api/v1/system/prune/start", "", admin); code != http.StatusAccepted {
		t.Fatalf("start: %d %s", code, body)
	}
	var st struct {
		MaxAgeHours, MaxMessages int
		NextRunAt                *time.Time
		LastRun                  *struct {
			FinishedAt    *time.Time
			Removed, Busy int
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		code, body, _ := c.do(http.MethodGet, "/api/v1/system/prune", "", admin)
		if code != http.StatusOK || json.Unmarshal([]byte(body), &st) != nil {
			t.Fatalf("status: %d %s", code, body)
		}
		if st.LastRun != nil && st.LastRun.FinishedAt != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the pass did not end: %s", body)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Age removes old-1 and old-2; the count then removes the oldest of
	// the four new ones.
	if st.MaxAgeHours != 24 || st.MaxMessages != 3 || st.NextRunAt == nil || st.LastRun.Removed != 3 || st.LastRun.Busy != 0 {
		t.Errorf("status = %+v, last run %+v", st, *st.LastRun)
	}
	_, body, header := c.do(http.MethodGet, "/api/v1/messages?sort=receivedAt", "", admin)
	if header.Get("X-Total-Count") != "3" || strings.Contains(body, "old-") || strings.Contains(body, sent[0]) || !strings.Contains(body, sent[3]) {
		t.Errorf("left (%s): %s", header.Get("X-Total-Count"), body)
	}
	if _, body, _ := c.do(http.MethodGet, "/api/v1/events?type=messages.pruned", "", admin); !strings.Contains(body, `"removed":"3"`) {
		t.Errorf("messages.pruned event: %s", body)
	}
}
