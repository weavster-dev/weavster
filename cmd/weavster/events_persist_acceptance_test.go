package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestEventsSurviveRestart: events from real processing are still returned
// after the server restarts (search and get by id), and new events get ids
// after them.
func TestEventsSurviveRestart(t *testing.T) {
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\n"+storeConfig(t))
	args := []string{"server", "--config", cfg}
	stop := startCLI(t, args, "http://"+addr+"/api/openapi.yaml")
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)

	createFlow(t, c, `{"id":"adt","destinations":[{"name":"out","type":"file","dir":"`+t.TempDir()+`"}]}`)
	sendMessage(t, c, "adt", `{"k":"v"}`)
	type event struct {
		ID     int64
		Type   string
		FlowID string
	}
	events := func() []event {
		t.Helper()
		code, body, _ := c.do(http.MethodGet, "/api/v1/events?flowId=adt", "", admin)
		var out []event
		if code != http.StatusOK || json.Unmarshal([]byte(body), &out) != nil {
			t.Fatalf("events: %d %s", code, body)
		}
		return out
	}
	before := events()
	if len(before) == 0 {
		t.Fatal("no events for the processed message")
	}
	time.Sleep(2 * eventFlushEvery) // written in the background
	stop()
	if !restartable(t) {
		return
	}

	stop = startCLI(t, args, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	after := events()
	if len(after) < len(before) || after[0].ID != before[0].ID || after[len(before)-1].Type != before[len(before)-1].Type {
		t.Fatalf("events after a restart = %+v, want %+v first", after, before)
	}
	if code, body, _ := c.do(http.MethodGet, fmt.Sprintf("/api/v1/events/%d", before[0].ID), "", admin); code != http.StatusOK {
		t.Errorf("get event %d after a restart: %d %s", before[0].ID, code, body)
	}
	sendMessage(t, c, "adt", `{"k":"w"}`)
	if now := events(); now[len(now)-1].ID <= before[len(before)-1].ID {
		t.Errorf("new event id %d does not follow %d", now[len(now)-1].ID, before[len(before)-1].ID)
	}
}
