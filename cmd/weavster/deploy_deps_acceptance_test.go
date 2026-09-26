package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestDeployDependenciesAndDeleteRunning: deploying a flow deploys its
// undeployed dependencies (chain), leaves already-deployed ones alone, and a
// started flow can be deleted (undeployed first, then removed).
func TestDeployDependenciesAndDeleteRunning(t *testing.T) {
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n")
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	stop := startCLI(t, []string{"server", "--config", cfg}, c.base+"/api/openapi.yaml")
	defer stop()

	for _, body := range []string{`{"id":"base"}`, `{"id":"other"}`, `{"id":"mid","dependsOn":["base","other"]}`, `{"id":"top","dependsOn":["mid"]}`} {
		if code, resp, _ := c.do(http.MethodPost, "/api/v1/flows", body, admin); code != http.StatusCreated {
			t.Fatalf("create %s: %d %q", body, code, resp)
		}
	}
	startFlow(t, c, "other") // already running: must stay started
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows/top/deploy", "", admin); code != http.StatusOK || !strings.Contains(body, `"status":"deployed"`) {
		t.Fatalf("deploy top: %d %q", code, body)
	}
	for id, want := range map[string]string{"top": "deployed", "mid": "deployed", "base": "deployed", "other": "started"} {
		if _, body, _ := c.do(http.MethodGet, "/api/v1/flows/"+id, "", admin); !strings.Contains(body, `"status":"`+want+`"`) {
			t.Errorf("%s = %s, want %s", id, body, want)
		}
	}

	// Delete a started flow: it is undeployed and removed; its messages stop.
	createFlow(t, c, `{"id":"live"}`)
	if _, status := sendMessage(t, c, "live", "x"); status != "sent" {
		t.Fatalf("message before delete: %s", status)
	}
	if code, _, _ := c.do(http.MethodDelete, "/api/v1/flows/live", "", admin); code != http.StatusNoContent {
		t.Fatalf("delete started flow: %d", code)
	}
	if code, _, _ := c.do(http.MethodPost, "/api/v1/flows/live/messages", "x", admin); code != http.StatusNotFound {
		t.Errorf("message after delete: %d, want 404", code)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, body, _ := c.do(http.MethodGet, "/api/v1/events?flowId=live", "", admin)
		undeployed := strings.Index(body, `"type":"flow.undeployed"`)
		deleted := strings.Index(body, `"type":"flow.deleted"`)
		if undeployed >= 0 && deleted > undeployed && strings.Contains(body, `"type":"flow.started"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("want flow.started, then flow.undeployed before flow.deleted: %s", body)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
