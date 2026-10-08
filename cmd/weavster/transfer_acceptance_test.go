package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestFlowExportImport exports flows (with dependencies) from one server and
// imports them into another, covering validation, conflicts, overwrite, and
// delete protection. The servers run one after the other: they share this
// test process, and stopping one sends SIGTERM to the whole process.
func TestFlowExportImport(t *testing.T) {
	admin := basic(bootstrapAdmin, testAdminPassword)
	start := func() (apiClient, func()) {
		addr := freeAddr(t)
		cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\n"+storeConfig(t))
		c := apiClient{t: t, base: "http://" + addr}
		return c, startCLI(t, []string{"server", "--config", cfg}, c.base+"/api/openapi.yaml")
	}
	src, stopSrc := start()

	// base <- mid <- top (top depends on mid, mid on base); other is separate.
	createFlow(t, src, `{"id":"base","name":"Base"}`)
	createFlow(t, src, `{"id":"mid","dependsOn":["base"]}`)
	createFlow(t, src, `{"id":"top","dependsOn":["mid"],"destinations":[{"name":"d","type":"file","dir":"/tmp/x"}]}`)
	createFlow(t, src, `{"id":"other"}`)

	for _, tc := range []struct{ name, body, want string }{
		{"unknown dependency", `{"id":"x","dependsOn":["nope"]}`, "depends on unknown flow nope"},
		{"self dependency", `{"id":"x","dependsOn":["x"]}`, "cannot depend on itself"},
		{"reserved id", `{"id":"export"}`, "is reserved"},
	} {
		if code, body, _ := src.do(http.MethodPost, "/api/v1/flows", tc.body, admin); code != http.StatusBadRequest || !strings.Contains(body, tc.want) {
			t.Errorf("%s: %d %q", tc.name, code, body)
		}
	}
	if code, body, _ := src.do(http.MethodPut, "/api/v1/flows/base", `{"dependsOn":["top"]}`, admin); code != http.StatusBadRequest || !strings.Contains(body, "dependency cycle") {
		t.Errorf("cycle via update: %d %q", code, body)
	}
	if code, body, _ := src.do(http.MethodDelete, "/api/v1/flows/base", "", admin); code != http.StatusConflict || !strings.Contains(body, "depended on by mid") {
		t.Errorf("delete dependency: %d %q", code, body)
	}

	code, exported, _ := src.do(http.MethodGet, "/api/v1/flows/export?ids=top", "", admin)
	var bundle struct {
		Version int
		Flows   []map[string]any
	}
	if err := json.Unmarshal([]byte(exported), &bundle); code != http.StatusOK || err != nil || bundle.Version != 1 || len(bundle.Flows) != 3 {
		t.Fatalf("export: %d %s", code, exported)
	}
	if strings.Contains(exported, `"status"`) || strings.Contains(exported, `"other"`) {
		t.Errorf("export should carry definitions of top and its dependencies only: %s", exported)
	}
	if code, _, _ := src.do(http.MethodGet, "/api/v1/flows/export?ids=nope", "", admin); code != http.StatusNotFound {
		t.Errorf("export unknown: %d", code)
	}
	if _, all, _ := src.do(http.MethodGet, "/api/v1/flows/export", "", admin); strings.Count(all, `"id":`) != 4 {
		t.Errorf("export all: %s", all)
	}

	stopSrc()

	dst, stopDst := start()
	defer stopDst()
	for _, tc := range []struct{ name, body, want string }{
		{"missing dependency", `{"version":1,"flows":[{"id":"mid","dependsOn":["base"]}]}`, "depends on unknown flow base"},
		{"bad version", `{"version":2,"flows":[]}`, "unsupported export version 2"},
		{"status", `{"version":1,"flows":[{"id":"a","status":"started"}]}`, "status is managed"},
		{"duplicate", `{"version":1,"flows":[{"id":"a"},{"id":"a"}]}`, "appears twice"},
		{"not a document", `[]`, "export document"},
	} {
		if code, body, _ := dst.do(http.MethodPost, "/api/v1/flows/import", tc.body, admin); code != http.StatusBadRequest || !strings.Contains(body, tc.want) {
			t.Errorf("%s: %d %q", tc.name, code, body)
		}
	}
	code, body, _ := dst.do(http.MethodPost, "/api/v1/flows/import", exported, admin)
	if code != http.StatusOK || !strings.Contains(body, `"created":["base","mid","top"]`) {
		t.Fatalf("import: %d %q", code, body)
	}
	if _, body, _ := dst.do(http.MethodGet, "/api/v1/flows/top", "", admin); !strings.Contains(body, `"status":"undeployed"`) || !strings.Contains(body, `"dependsOn":["mid"]`) {
		t.Errorf("imported top = %s", body)
	}

	// Re-import: conflict unless overwrite; overwrite keeps status.
	if code, body, _ := dst.do(http.MethodPost, "/api/v1/flows/import", exported, admin); code != http.StatusConflict || !strings.Contains(body, "base, mid, top") {
		t.Errorf("re-import: %d %q", code, body)
	}
	startFlow(t, dst, "base")
	renamed := strings.Replace(exported, `"name":"Base"`, `"name":"Base v2"`, 1)
	if code, body, _ := dst.do(http.MethodPost, "/api/v1/flows/import?overwrite=true", renamed, admin); code != http.StatusOK || !strings.Contains(body, `"updated":["base","mid","top"]`) {
		t.Errorf("overwrite: %d %q", code, body)
	}
	if _, body, _ := dst.do(http.MethodGet, "/api/v1/flows/base", "", admin); !strings.Contains(body, `"name":"Base v2"`) || !strings.Contains(body, `"status":"started"`) {
		t.Errorf("overwritten base = %s", body)
	}
	if code, _, _ := dst.do(http.MethodPost, "/api/v1/flows/import?overwrite=maybe", exported, admin); code != http.StatusBadRequest {
		t.Errorf("bad overwrite: %d", code)
	}
}
