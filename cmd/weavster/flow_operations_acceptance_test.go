package main

import (
	"net/http"
	"strings"
	"testing"
)

// TestFlowOperations covers connector names, ports-in-use, bulk update,
// and initial state (auto-deploy at the next start) through the binary.
func TestFlowOperations(t *testing.T) {
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n")
	args := []string{"server", "--config", cfg}
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	stop := startCLI(t, args, c.base+"/api/openapi.yaml")

	for _, body := range []string{
		`{"id":"d","name":"Default","enabled":true,"sourceType":"http","destinations":[{"name":"out","type":"file","dir":"` + t.TempDir() + `"}]}`,
		`{"id":"p","enabled":true,"initialState":"paused"}`,
		`{"id":"s","enabled":true,"initialState":"stopped"}`,
		`{"id":"q","enabled":true}`,
	} {
		if code, resp, _ := c.do(http.MethodPost, "/api/v1/flows", body, admin); code != http.StatusCreated {
			t.Fatalf("create %s: %d %q", body, code, resp)
		}
	}
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"x","initialState":"halted"}`, admin); code != http.StatusBadRequest {
		t.Errorf("initialState halted: %d %q, want 400", code, body)
	}

	if _, body, _ := c.do(http.MethodGet, "/api/v1/flows/connector-names", "", admin); !strings.Contains(body, `{"id":"d","name":"Default","sourceType":"http","destinations":["out"]}`) {
		t.Errorf("connector-names = %s", body)
	}
	if _, body, _ := c.do(http.MethodGet, "/api/v1/flows/ports-in-use", "", admin); !strings.Contains(body, `"address":"`+addr+`"`) || !strings.Contains(body, `"usedBy":"api"`) {
		t.Errorf("ports-in-use = %s", body)
	}

	// Nothing is written when one flow is unknown.
	if code, body, _ := c.do(http.MethodPut, "/api/v1/flows", `{"flows":[{"id":"d","name":"Renamed"},{"id":"nope"}]}`, admin); code != http.StatusNotFound || !strings.Contains(body, "nope") {
		t.Errorf("bulk update with unknown flow: %d %q, want 404 naming it", code, body)
	}
	if _, body, _ := c.do(http.MethodGet, "/api/v1/flows/d", "", admin); !strings.Contains(body, `"name":"Default"`) {
		t.Errorf("failed bulk update wrote d: %s", body)
	}
	// enabled is kept unless set; q is disabled explicitly.
	if code, body, _ := c.do(http.MethodPut, "/api/v1/flows", `{"flows":[{"id":"d","name":"Renamed","sourceType":"http"},{"id":"q","enabled":false}]}`, admin); code != http.StatusOK || body != `{"updated":["d","q"]}`+"\n" {
		t.Fatalf("bulk update: %d %q", code, body)
	}
	if _, body, _ := c.do(http.MethodGet, "/api/v1/flows/d", "", admin); !strings.Contains(body, `"name":"Renamed"`) || !strings.Contains(body, `"enabled":true`) || strings.Contains(body, "destinations") {
		t.Errorf("after bulk update d = %s", body)
	}
	stop()

	stop = startCLI(t, args, c.base+"/api/openapi.yaml")
	defer stop()
	for id, want := range map[string]string{"d": "started", "p": "paused", "s": "stopped", "q": "undeployed"} {
		if _, body, _ := c.do(http.MethodGet, "/api/v1/flows/"+id, "", admin); !strings.Contains(body, `"status":"`+want+`"`) {
			t.Errorf("after restart %s = %s, want status %s", id, body, want)
		}
	}
}
