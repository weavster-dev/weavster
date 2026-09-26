package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestFlowLifecycle drives every lifecycle operation over the API against a
// sqlite store, checks invalid transitions and message acceptance, and that
// the state survives a restart.
func TestFlowLifecycle(t *testing.T) {
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n")
	args := []string{"server", "--config", cfg}
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	stop := startCLI(t, args, c.base+"/api/openapi.yaml")

	for _, status := range []string{`"started"`, `""`, `null`} {
		if code, body, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"f","status":`+status+`}`, admin); code != http.StatusBadRequest || !strings.Contains(body, "status is managed") {
			t.Errorf("create with status %s: %d %q, want 400", status, code, body)
		}
	}
	if status, body, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"f"}`, admin); status != http.StatusCreated || !strings.Contains(body, `"status":"undeployed"`) {
		t.Fatalf("create: %d %q", status, body)
	}

	steps := []struct {
		action, wantStatus string
		wantCode           int
		acceptsMessages    bool
	}{
		{"start", "", http.StatusConflict, false},
		{"deploy", "deployed", http.StatusOK, false},
		{"deploy", "", http.StatusConflict, false},
		{"start", "started", http.StatusOK, true},
		{"pause", "paused", http.StatusOK, false},
		{"halt", "", http.StatusConflict, false},
		{"resume", "started", http.StatusOK, true},
		{"halt", "halted", http.StatusOK, false},
		{"resume", "started", http.StatusOK, true},
		{"stop", "stopped", http.StatusOK, false},
		{"start", "started", http.StatusOK, true},
		{"undeploy", "undeployed", http.StatusOK, false},
		{"undeploy", "", http.StatusConflict, false},
	}
	for _, st := range steps {
		code, body, _ := c.do(http.MethodPost, "/api/v1/flows/f/"+st.action, "", admin)
		if code != st.wantCode || (st.wantStatus != "" && !strings.Contains(body, `"status":"`+st.wantStatus+`"`)) {
			t.Fatalf("%s: %d %q, want %d %s", st.action, code, body, st.wantCode, st.wantStatus)
		}
		if code == http.StatusConflict && !strings.Contains(body, "cannot "+st.action) {
			t.Errorf("%s conflict message = %q", st.action, body)
		}
		if code != http.StatusOK {
			continue
		}
		msgCode, _, _ := c.do(http.MethodPost, "/api/v1/flows/f/messages", "x", admin)
		if st.acceptsMessages != (msgCode == http.StatusAccepted) {
			t.Errorf("after %s: message → %d, accepts=%v", st.action, msgCode, st.acceptsMessages)
		}
	}
	if code, _, _ := c.do(http.MethodPost, "/api/v1/flows/nope/deploy", "", admin); code != http.StatusNotFound {
		t.Errorf("unknown flow: %d, want 404", code)
	}
	if code, _, _ := c.do(http.MethodPost, "/api/v1/flows/f/explode", "", admin); code != http.StatusNotFound {
		t.Errorf("unknown action: %d, want 404", code)
	}

	// redeploy-all: started and stopped flows end deployed; undeployed flows
	// are untouched.
	createFlow(t, c, `{"id":"g"}`) // started
	createFlow(t, c, `{"id":"h"}`)
	c.do(http.MethodPost, "/api/v1/flows/h/stop", "", admin) // stopped
	code, body, _ := c.do(http.MethodPost, "/api/v1/flows/redeploy-all", "", admin)
	var redeployed []struct{ ID, Status string }
	if err := json.Unmarshal([]byte(body), &redeployed); code != http.StatusOK || err != nil || len(redeployed) != 2 {
		t.Fatalf("redeploy-all: %d %q", code, body)
	}
	for _, f := range redeployed {
		if f.Status != "deployed" || f.ID == "f" {
			t.Errorf("redeploy-all result %+v", f)
		}
	}
	c.do(http.MethodPost, "/api/v1/flows/g/start", "", admin)
	stop()

	// State survives a restart.
	stop = startCLI(t, args, c.base+"/api/openapi.yaml")
	defer stop()
	for id, want := range map[string]string{"f": "undeployed", "g": "started", "h": "deployed"} {
		if _, body, _ := c.do(http.MethodGet, "/api/v1/flows/"+id, "", admin); !strings.Contains(body, `"status":"`+want+`"`) {
			t.Errorf("after restart %s = %s, want %s", id, body, want)
		}
	}
}
