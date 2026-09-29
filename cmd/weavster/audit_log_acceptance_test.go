package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestAuditLogStored: audit entries (a change, PHI reads, a failed login)
// are stored, redacted as on stderr, and searchable with audit:view by
// action, actor, and cursor; reading them is itself audited; and they are
// still there after a restart.
func TestAuditLogStored(t *testing.T) {
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\n"+storeConfig(t))
	args := []string{"server", "--config", cfg}
	stop := startCLI(t, args, "http://"+addr+"/api/openapi.yaml")
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)

	createFlow(t, c, `{"id":"adt"}`)
	id, _ := sendMessage(t, c, "adt", `{"k":"v"}`)
	c.do(http.MethodGet, "/api/v1/messages?flowId=adt&apiToken=s3cr3t", "", admin)
	c.do(http.MethodGet, "/api/v1/flows", "", basic(bootstrapAdmin, "wrong-password"))
	if code, body, _ := c.do(http.MethodPost, "/api/v1/users",
		`{"username":"viewer","password":"Viewer-Pass-1","permissions":["flows:view"],"mustChangePassword":false}`, admin); code != http.StatusCreated {
		t.Fatalf("create viewer: %d %s", code, body)
	}

	time.Sleep(auditSettle + 100*time.Millisecond) // entries are searchable once settled

	type entry struct {
		ID                      int64
		Actor, Action, Resource string
		Detail                  map[string]string
	}
	search := func(query string) []entry {
		t.Helper()
		code, body, _ := c.do(http.MethodGet, "/api/v1/audit?"+query, "", admin)
		var out []entry
		if code != http.StatusOK || json.Unmarshal([]byte(body), &out) != nil {
			t.Fatalf("audit %s: %d %s", query, code, body)
		}
		return out
	}
	phi := search("action=phi.access")
	if len(phi) != 1 || phi[0].Actor != bootstrapAdmin || phi[0].Detail["messages.ids"] != `["`+id+`"]` || phi[0].Detail["query.apiToken"] != "[redacted]" {
		t.Errorf("phi.access entries = %+v", phi)
	}
	if got := search("action=auth.failure"); len(got) != 1 || got[0].Resource != "/api/v1/flows" {
		t.Errorf("auth.failure entries = %+v", got)
	}
	if got := search("action=POST+/api/v1/flows"); len(got) != 1 || got[0].Detail["status"] != "201" {
		t.Errorf("flow creation entries = %+v", got)
	}
	all := search("limit=1000")
	if len(all) < 5 {
		t.Fatalf("all entries = %+v", all)
	}
	if page := search(fmt.Sprintf("afterId=%d&limit=2", all[0].ID)); len(page) != 2 || page[0].ID != all[1].ID {
		t.Errorf("page after %d = %+v", all[0].ID, page)
	}
	if code, body, _ := c.do(http.MethodGet, "/api/v1/audit", "", basic("viewer", "Viewer-Pass-1")); code != http.StatusForbidden || !strings.Contains(body, "audit:view") {
		t.Errorf("audit without audit:view: %d %s", code, body)
	}
	if code, body, _ := c.do(http.MethodGet, "/api/v1/audit?limit=5000", "", admin); code != http.StatusBadRequest {
		t.Errorf("limit 5000: %d %s", code, body)
	}
	// Searches without an action filter leave audit reads out.
	if got := search("actor=" + bootstrapAdmin + "&limit=1000"); len(got) == 0 || got[len(got)-1].Action == "audit.read" {
		t.Errorf("audit reads in an unfiltered search: %+v", got)
	}
	time.Sleep(auditSettle + 100*time.Millisecond)
	if got := search("action=audit.read"); len(got) == 0 || got[0].Detail["entries"] == "" {
		t.Errorf("reading the audit log was not audited: %+v", got)
	}
	// A search that finds nothing discloses nothing and is not recorded.
	search("actor=nobody")
	time.Sleep(auditSettle + 100*time.Millisecond)
	for _, e := range search("action=audit.read&limit=1000") {
		if strings.Contains(e.Resource, "nobody") || e.Detail["query.actor"] == "nobody" {
			t.Errorf("an empty search was recorded: %+v", e)
		}
	}

	stop()
	if !restartable(t) {
		return
	}
	stop = startCLI(t, args, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	if got := search("action=phi.access"); len(got) != 1 || got[0].Detail["messages.ids"] != `["`+id+`"]` {
		t.Errorf("phi.access after a restart = %+v", got)
	}
}
