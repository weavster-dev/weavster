package main

import (
	"net/http"
	"strings"
	"testing"
)

// TestFlowUpdateAndEnable covers PUT /flows/{id}, rename, enable/disable,
// and auto-deploy of enabled flows on restart (flows.deployOnStartup).
func TestFlowUpdateAndEnable(t *testing.T) {
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n")
	args := []string{"server", "--config", cfg}
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	stop := startCLI(t, args, c.base+"/api/openapi.yaml")

	createFlow(t, c, `{"id":"f","name":"Old"}`) // started
	code, body, _ := c.do(http.MethodPut, "/api/v1/flows/f", `{"name":"Renamed","transform":{"name":"t","steps":[{"set":{"field":"x","expr":"y"}}]}}`, admin)
	if code != http.StatusOK || !strings.Contains(body, `"name":"Renamed"`) || !strings.Contains(body, `"status":"started"`) {
		t.Fatalf("update: %d %q", code, body)
	}
	// The new transform applies to messages received afterwards.
	if _, status := sendMessage(t, c, "f", `{}`); status != "sent" {
		t.Errorf("message after update: %s", status)
	}

	// Omitting "enabled" keeps it; sending it changes it.
	c.do(http.MethodPost, "/api/v1/flows/f/enable", "", admin)
	if _, body, _ := c.do(http.MethodPut, "/api/v1/flows/f", `{"name":"Renamed again"}`, admin); !strings.Contains(body, `"enabled":true`) {
		t.Errorf("update without enabled dropped it: %s", body)
	}
	if _, body, _ := c.do(http.MethodPut, "/api/v1/flows/f", `{"name":"Renamed again","enabled":false}`, admin); !strings.Contains(body, `"enabled":false`) {
		t.Errorf("update with enabled:false kept it: %s", body)
	}

	for _, tc := range []struct{ name, body, want string }{
		{"null body", `null`, "must be a JSON object"},
		{"status key", `{"name":"x","status":"stopped"}`, "status is managed"},
		{"id change", `{"id":"g","name":"x"}`, "cannot be changed"},
		{"bad transform", `{"transform":{"name":"t","steps":[{"build":{"template":"x"}}]}}`, "not supported yet"},
	} {
		if code, body, _ := c.do(http.MethodPut, "/api/v1/flows/f", tc.body, admin); code != http.StatusBadRequest || !strings.Contains(body, tc.want) {
			t.Errorf("%s: %d %q, want 400 %q", tc.name, code, body, tc.want)
		}
	}
	if code, body, _ := c.do(http.MethodPut, "/api/v1/flows/f", `{"id":null,"name":"Null id"}`, admin); code != http.StatusOK || !strings.Contains(body, `"id":"f"`) {
		t.Errorf("update with null id: %d %q", code, body)
	}
	if code, _, _ := c.do(http.MethodPut, "/api/v1/flows/nope", `{"name":"x"}`, admin); code != http.StatusNotFound {
		t.Errorf("update unknown: %d, want 404", code)
	}

	// enable/disable leave the status alone.
	c.do(http.MethodPost, "/api/v1/flows", `{"id":"auto"}`, admin)
	c.do(http.MethodPost, "/api/v1/flows", `{"id":"manual","enabled":true}`, admin)
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows/auto/enable", "", admin); code != http.StatusOK || !strings.Contains(body, `"enabled":true`) || !strings.Contains(body, `"status":"undeployed"`) {
		t.Errorf("enable: %d %q", code, body)
	}
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows/manual/disable", "", admin); code != http.StatusOK || !strings.Contains(body, `"enabled":false`) {
		t.Errorf("disable: %d %q", code, body)
	}
	if code, _, _ := c.do(http.MethodPost, "/api/v1/flows/nope/enable", "", admin); code != http.StatusNotFound {
		t.Errorf("enable unknown: %d, want 404", code)
	}
	stop()

	// Restart: the enabled, undeployed flow is deployed and started; the
	// disabled one is left undeployed; f keeps its state.
	stop = startCLI(t, args, c.base+"/api/openapi.yaml")
	defer stop()
	for id, want := range map[string]string{"auto": "started", "manual": "undeployed", "f": "started"} {
		if _, body, _ := c.do(http.MethodGet, "/api/v1/flows/"+id, "", admin); !strings.Contains(body, `"status":"`+want+`"`) {
			t.Errorf("after restart %s = %s, want %s", id, body, want)
		}
	}
}

// TestDeployOnStartupDisabled: with flows.deployOnStartup false, enabled
// flows stay undeployed across a restart.
func TestDeployOnStartupDisabled(t *testing.T) {
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\nflows: {deployOnStartup: false}\n")
	args := []string{"server", "--config", cfg}
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	stop := startCLI(t, args, c.base+"/api/openapi.yaml")
	c.do(http.MethodPost, "/api/v1/flows", `{"id":"auto","enabled":true}`, admin)
	stop()
	stop = startCLI(t, args, c.base+"/api/openapi.yaml")
	defer stop()
	if _, body, _ := c.do(http.MethodGet, "/api/v1/flows/auto", "", admin); !strings.Contains(body, `"status":"undeployed"`) {
		t.Errorf("auto = %s, want undeployed", body)
	}
}
