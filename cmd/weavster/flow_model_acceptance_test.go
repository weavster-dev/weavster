package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/config"
)

// TestConfigFlowIsAPIFlow: a flow from a config-as-code document is the
// same model the flow API accepts; the API stores it and returns every
// field unchanged.
func TestConfigFlowIsAPIFlow(t *testing.T) {
	doc := `
version: "1"
flows:
  adt:
    name: ADT normalize
    sourceType: http
    enabled: true
    initialState: paused
    responseSelector: ehr
    transform:
      steps:
        - map: {from: PID.5.1, to: patient.lastName}
    destinations:
      - name: ehr
        type: http
        url: https://ehr.example.com/inbound
        responseTransform:
          steps:
            - map: {from: code, to: ack}
      - name: archive
        type: file
        dir: /var/lib/weavster/archive
        transform:
          steps:
            - filter: {when: "patient.lastName == ''", action: reject}
`
	if err := config.Validate([]byte(doc)); err != nil {
		t.Fatalf("config rejected: %v", err)
	}
	cfg, err := config.Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(cfg.Flows["adt"])
	if err != nil {
		t.Fatal(err)
	}

	addr := freeAddr(t)
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	stop := startCLI(t, []string{"server", "--config", writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: memory}\n")}, c.base+"/api/openapi.yaml")
	defer stop()
	if code, resp, _ := c.do(http.MethodPost, "/api/v1/flows", string(body), admin); code != http.StatusCreated {
		t.Fatalf("create from config flow: %d %q", code, resp)
	}
	_, got, _ := c.do(http.MethodGet, "/api/v1/flows/adt", "", admin)
	want := strings.TrimSuffix(string(body), "}") + `,"status":"undeployed"}`
	var gotV, wantV any
	_ = json.Unmarshal([]byte(got), &gotV)
	_ = json.Unmarshal([]byte(want), &wantV)
	g, _ := json.Marshal(gotV)
	w, _ := json.Marshal(wantV)
	if string(g) != string(w) {
		t.Errorf("stored flow differs from the config flow:\n got %s\nwant %s", g, w)
	}
}
