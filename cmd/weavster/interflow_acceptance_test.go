package main

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

// TestFlowDestination: a flow destination hands messages to another flow
// in-process (with provenance metadata); a stopped target queues the
// delivery until it starts; routes follow the dependsOn rules (targets
// exist, no cycles, a target in use cannot be deleted).
func TestFlowDestination(t *testing.T) {
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\n"+storeConfig(t)+
		"delivery: {maxAttempts: 50, backoffBaseMs: 10, retryIntervalMs: 20}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	out := t.TempDir()
	createFlow(t, c, `{"id":"store","transform":{"steps":[{"set":{"field":"stored","expr":"yes"}}]},"destinations":[{"name":"out","type":"file","dir":"`+out+`"}]}`)
	createFlow(t, c, `{"id":"intake","transform":{"steps":[{"map":{"from":"patient","to":"who"}}]},"destinations":[{"name":"next","type":"flow","flow":"store"}]}`)

	id, status := sendMessage(t, c, "intake", `{"patient":"DOE"}`)
	if status != "sent" {
		t.Fatalf("intake: %s", status)
	}
	files, _ := os.ReadDir(out)
	if len(files) != 1 {
		t.Fatalf("store delivered %d files", len(files))
	}
	body, _ := os.ReadFile(out + "/" + files[0].Name())
	if string(body) != `{"patient":"DOE","stored":"yes","who":"DOE"}` {
		t.Errorf("store wrote %s", body)
	}
	if _, msgs, _ := c.do(http.MethodGet, "/api/v1/messages?flowId=store", "", admin); !strings.Contains(msgs, `"source.flow":"intake"`) || !strings.Contains(msgs, `"source.message":"`+id+`"`) {
		t.Errorf("store messages = %s", msgs)
	}

	// A stopped target queues the delivery; starting it delivers.
	if code, b, _ := c.do(http.MethodPost, "/api/v1/flows/store/stop", "", admin); code != http.StatusOK {
		t.Fatalf("stop: %d %s", code, b)
	}
	id2, status := sendMessage(t, c, "intake", `{"patient":"ROE"}`)
	if status != "queued" {
		t.Errorf("with the target stopped: %s, want queued", status)
	}
	c.do(http.MethodPost, "/api/v1/flows/store/start", "", admin)
	waitStatus(t, c, id2, "sent")
	if files, _ := os.ReadDir(out); len(files) != 2 {
		t.Errorf("store delivered %d files after start, want 2", len(files))
	}

	for method, tt := range map[string]struct{ path, body, want string }{
		"cycle":     {"/api/v1/flows/store", `{"id":"store","destinations":[{"name":"back","type":"flow","flow":"intake"}]}`, "cycle"},
		"self":      {"/api/v1/flows", `{"id":"loop","destinations":[{"name":"me","type":"flow","flow":"loop"}]}`, "cannot depend on itself"},
		"unknown":   {"/api/v1/flows", `{"id":"lost","destinations":[{"name":"x","type":"flow","flow":"nowhere"}]}`, "unknown flow nowhere"},
		"no target": {"/api/v1/flows", `{"id":"bad","destinations":[{"name":"x","type":"flow"}]}`, "flow.schema.json"},
	} {
		verb := http.MethodPost
		if tt.path == "/api/v1/flows/store" {
			verb = http.MethodPut
		}
		if code, resp, _ := c.do(verb, tt.path, tt.body, admin); code != http.StatusBadRequest || !strings.Contains(resp, tt.want) {
			t.Errorf("%s: %d %s", method, code, resp)
		}
	}
	if code, resp, _ := c.do(http.MethodDelete, "/api/v1/flows/store", "", admin); code != http.StatusConflict || !strings.Contains(resp, "intake") {
		t.Errorf("deleting a target in use: %d %s", code, resp)
	}

	// What the sender sends must be readable by the target, whichever of
	// the two changes.
	createFlow(t, c, `{"id":"hl7-in","inputFormat":"hl7v2"}`)
	if code, resp, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"json-out","transform":{"steps":[{"set":{"field":"a","expr":"b"}}]},"destinations":[{"name":"x","type":"flow","flow":"hl7-in"}]}`, admin); code != http.StatusBadRequest || !strings.Contains(resp, "sends json, which flow hl7-in (inputFormat hl7v2) cannot read") {
		t.Errorf("json to an hl7v2 flow: %d %s", code, resp)
	}
	if code, resp, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"xml-pass","inputFormat":"xml","destinations":[{"name":"x","type":"flow","flow":"hl7-in"}]}`, admin); code != http.StatusBadRequest || !strings.Contains(resp, "sends xml, which flow hl7-in (inputFormat hl7v2) cannot read") {
		t.Errorf("xml passthrough to an hl7v2 flow: %d %s", code, resp)
	}
	if code, resp, _ := c.do(http.MethodPut, "/api/v1/flows/store", `{"id":"store","inputFormat":"xml","destinations":[{"name":"out","type":"file","dir":"`+out+`"}]}`, admin); code != http.StatusBadRequest || !strings.Contains(resp, "which flow store (inputFormat xml) cannot read") {
		t.Errorf("changing the target's format under a sender: %d %s", code, resp)
	}

	// The topology shows the route.
	if _, topo, _ := c.do(http.MethodGet, "/api/v1/topology", "", admin); !strings.Contains(topo, `"from":"flow:intake","to":"flow:store","kind":"route"`) {
		t.Errorf("topology = %s", topo)
	}
}
