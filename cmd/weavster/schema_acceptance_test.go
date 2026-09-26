package main

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestFlowSchemaEnforced: create, update, and import reject definitions that
// do not match agent-docs/schemas/flow.schema.json.
func TestFlowSchemaEnforced(t *testing.T) {
	c := startComposed(t, serverconfig.Default(), io.Discard)
	admin := basic(bootstrapAdmin, testAdminPassword)
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"ok","name":"OK"}`, admin); code != http.StatusCreated {
		t.Fatalf("valid flow: %d %q", code, body)
	}
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"x","stoppedDestinations":["d"]}`, admin); code != http.StatusBadRequest || !strings.Contains(body, "managed by POST") {
		t.Errorf("create with stoppedDestinations: %d %q", code, body)
	}
	bad := []struct{ name, body, want string }{
		{"unknown field", `{"id":"x","destination":[]}`, "destination"},
		{"wrong type", `{"id":"x","dependsOn":"ok"}`, "/dependsOn"},
		{"bad destination type", `{"id":"x","destinations":[{"name":"d","type":"ftp","url":"ftp://x"}]}`, "/destinations/0/type"},
	}
	for _, tc := range bad {
		if code, body, _ := c.do(http.MethodPost, "/api/v1/flows", tc.body, admin); code != http.StatusBadRequest || !strings.Contains(body, "flow.schema.json") || !strings.Contains(body, tc.want) {
			t.Errorf("create %s: %d %q", tc.name, code, body)
		}
		upd := strings.Replace(tc.body, `"id":"x",`, "", 1)
		if code, body, _ := c.do(http.MethodPut, "/api/v1/flows/ok", upd, admin); code != http.StatusBadRequest || !strings.Contains(body, tc.want) {
			t.Errorf("update %s: %d %q", tc.name, code, body)
		}
		if code, body, _ := c.do(http.MethodPost, "/api/v1/flows/import", `{"version":1,"flows":[`+tc.body+`]}`, admin); code != http.StatusBadRequest || !strings.Contains(body, "flows[0]") || !strings.Contains(body, tc.want) {
			t.Errorf("import %s: %d %q", tc.name, code, body)
		}
	}
}
