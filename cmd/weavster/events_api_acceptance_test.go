package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestEventsAPI: events from real lifecycle actions and processing can be
// searched by time and cursor, read one by one, counted, and exported.
func TestEventsAPI(t *testing.T) {
	c := startComposed(t, serverconfig.Default(), io.Discard)
	admin := basic(bootstrapAdmin, testAdminPassword)
	start := time.Now().UTC().Add(-time.Second).Format(time.RFC3339)
	createFlow(t, c, `{"id":"adt","destinations":[{"name":"out","type":"file","dir":"`+t.TempDir()+`"}]}`)
	get := func(path string) (int, string) {
		code, body, _ := c.do(http.MethodGet, path, "", admin)
		return code, body
	}
	_, body := get("/api/v1/events/max-id")
	var mx struct{ MaxID int64 }
	if err := json.Unmarshal([]byte(body), &mx); err != nil || mx.MaxID < 1 {
		t.Fatalf("max-id = %s", body)
	}
	// Poll for new events from the last id seen.
	sendMessage(t, c, "adt", `{"k":"v"}`)
	sendMessage(t, c, "adt", `{"k":"w"}`)
	_, body = get(fmt.Sprintf("/api/v1/events?afterId=%d", mx.MaxID))
	var fresh []struct {
		ID   int64
		Type string
	}
	if err := json.Unmarshal([]byte(body), &fresh); err != nil || len(fresh) != 2 || fresh[0].Type != "message.sent" || fresh[0].ID != mx.MaxID+1 {
		t.Fatalf("new events = %s", body)
	}
	for _, tt := range []struct {
		path   string
		status int
		want   string
	}{
		{fmt.Sprintf("/api/v1/events/%d", fresh[1].ID), http.StatusOK, `"type":"message.sent","flowId":"adt"`},
		{"/api/v1/events/99999", http.StatusNotFound, "event not found"},
		{"/api/v1/events/abc", http.StatusBadRequest, "positive number"},
		{"/api/v1/events/count?type=message.sent&flowId=adt", http.StatusOK, `{"count":2}`},
		{"/api/v1/events/count?from=" + start, http.StatusOK, `"count":`},
		{"/api/v1/events/count?to=2000-01-01T00:00:00Z", http.StatusOK, `{"count":0}`},
		{"/api/v1/events?from=yesterday", http.StatusBadRequest, "RFC 3339"},
		{"/api/v1/events/export?type=message.sent", http.StatusOK, `"type":"message.sent"`},
	} {
		if code, body := get(tt.path); code != tt.status || !strings.Contains(body, tt.want) {
			t.Errorf("%s: %d %s; want %d containing %q", tt.path, code, body, tt.status, tt.want)
		}
	}
	c.do(http.MethodPost, "/api/v1/users", `{"username":"viewer","password":"Viewer-Passw0rd","permissions":["flows:view"],"mustChangePassword":false}`, admin)
	if code, body, _ := c.do(http.MethodGet, "/api/v1/events/count", "", basic("viewer", "Viewer-Passw0rd")); code != http.StatusForbidden || !strings.Contains(body, "events:view") {
		t.Errorf("viewer: %d %s", code, body)
	}
}
