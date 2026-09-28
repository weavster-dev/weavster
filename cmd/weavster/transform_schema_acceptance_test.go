package main

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTransformSchema: transforms are checked against the published
// transform.schema.json by the API (flow, destination, and response
// transforms) and by offline config validate; steps that do not run yet
// and malformed steps are refused with the schema path.
func TestTransformSchema(t *testing.T) {
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	for flow, want := range map[string]string{
		`{"id":"a","transform":{"steps":[{"destinationSet":{"exclude":["x"]}}]}}`:                                            "/transform/steps/0",
		`{"id":"a","transform":{"steps":[{"filter":{"when":"x","action":"drop"}}]}}`:                                         "/transform/steps/0/filter/action",
		`{"id":"a","destinations":[{"name":"d","type":"file","dir":"/tmp/x","transform":{"steps":[{"map":{"from":"a"}}]}}]}`: "/destinations/0/transform/steps/0/map",
		`{"id":"a","destinations":[{"name":"d","type":"http","url":"https://x","responseTransform":{"color":"red"}}]}`:       "/destinations/0/responseTransform",
	} {
		code, body, _ := c.do(http.MethodPost, "/api/v1/flows", flow, admin)
		if code != http.StatusBadRequest || !strings.Contains(body, "flow.schema.json") || !strings.Contains(body, want) {
			t.Errorf("%s: %d %s", flow, code, body)
		}
	}
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"ok","transform":{"name":"t","steps":[{"map":{"from":"a","to":"b","type":"number"}}]}}`, admin); code != http.StatusCreated {
		t.Errorf("valid transform: %d %s", code, body)
	}

	path := filepath.Join(t.TempDir(), "flows.yaml")
	doc := "version: \"1\"\nflows:\n  adt:\n    id: adt\n    transform:\n      steps:\n        - build: {template: x}\n"
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := run([]string{"config", "validate", path}, strings.NewReader(""), &out, &errb); code == 0 || !strings.Contains(errb.String(), "flows.adt: flow does not match flow.schema.json: /transform/steps/0") {
		t.Errorf("offline validate: %d %q %q", code, out.String(), errb.String())
	}
}
