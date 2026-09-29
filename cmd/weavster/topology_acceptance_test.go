package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/topology"
)

// TestTopologyGraphs: the running server's topology comes from the stored
// flows: the drill-down shows the source, the transform, and every
// destination with message-path edges and a route to another flow; a
// destination whose deliveries fail is errored, a stopped one stopped;
// activity counts are present even when zero; both graphs validate
// against the published schema; and activity survives a restart.
func TestTopologyGraphs(t *testing.T) {
	addr, src := freeAddr(t), freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstats: {sampleIntervalMs: 100, retentionHours: 1}\n"+
		"delivery: {maxAttempts: 2, backoffBaseMs: 3600000}\n"+storeConfig(t))
	args := []string{"server", "--config", cfg}
	stop := startCLI(t, args, "http://"+addr+"/api/openapi.yaml")
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)

	createFlow(t, c, `{"id":"billing","destinations":[{"name":"out","type":"file","dir":"`+t.TempDir()+`"}]}`)
	createFlow(t, c, `{"id":"adt","name":"ADT inbound","source":{"type":"http","address":"`+src+`","path":"/adt"},`+
		`"transform":{"name":"normalize","steps":[{"map":{"from":"k","to":"v"}}]},"destinations":[`+
		`{"name":"ehr","type":"http","url":"http://127.0.0.1:1/in"},{"name":"archive","type":"file","dir":"`+t.TempDir()+`"},`+
		`{"name":"tobilling","type":"flow","flow":"billing"}]}`)
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows/adt/destinations/tobilling/stop", "", admin); code != http.StatusOK {
		t.Fatalf("stop destination: %d %s", code, body)
	}
	time.Sleep(300 * time.Millisecond) // a sample before the traffic
	sendMessage(t, c, "adt", `{"k":"a"}`)
	sendMessage(t, c, "adt", `{"k":"b"}`)

	graph := func(path string) topology.Graph {
		t.Helper()
		code, body, _ := c.do(http.MethodGet, path, "", admin)
		var g topology.Graph
		if code != http.StatusOK || json.Unmarshal([]byte(body), &g) != nil {
			t.Fatalf("%s: %d %s", path, code, body)
		}
		if err := topology.Validate([]byte(body)); err != nil {
			t.Errorf("%s does not match the schema: %v", path, err)
		}
		if strings.Contains(body, `"activity":{`) && !strings.Contains(body, `"queued":`) {
			t.Errorf("%s: activity without zero counters: %s", path, body)
		}
		return g
	}
	node := func(g topology.Graph, id string) topology.Node {
		t.Helper()
		for _, n := range g.Nodes {
			if n.ID == id {
				return n
			}
		}
		t.Fatalf("no node %s in %+v", id, g.Nodes)
		return topology.Node{}
	}
	edge := func(g topology.Graph, from, to string) topology.Edge {
		t.Helper()
		for _, e := range g.Edges {
			if e.From == from && e.To == to {
				return e
			}
		}
		t.Fatalf("no edge %s -> %s in %+v", from, to, g.Edges)
		return topology.Edge{}
	}

	// Wait for a sample that shows the failed deliveries.
	var g topology.Graph
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		g = graph("/api/v1/topology/flows/flow:adt")
		if node(g, "destination:ehr").Status == "errored" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("destination:ehr never errored: %+v", g.Nodes)
		}
	}
	if g.FlowID != "flow:adt" || g.FlowName != "ADT inbound" || g.FlowStatus != "started" {
		t.Errorf("flow = %q %q %q", g.FlowID, g.FlowName, g.FlowStatus)
	}
	if n := node(g, "source:http"); n.Label != "http://"+src+"/adt" || n.Meta["connectorType"] != "http" || n.Meta["dataType"] != "json" || n.Activity.Received != 2 {
		t.Errorf("source = %+v %+v", n, n.Activity)
	}
	if n := node(g, "transform:dsl:normalize"); n.Meta["steps"] != "1" || n.Status != "started" {
		t.Errorf("transform = %+v", n)
	}
	if n := node(g, "destination:archive"); n.Status != "started" || n.Activity.Sent != 2 {
		t.Errorf("archive = %+v %+v", n, n.Activity)
	}
	if n := node(g, "destination:tobilling"); n.Status != "stopped" || n.Meta["flow"] != "billing" {
		t.Errorf("tobilling = %+v", n)
	}
	for to, want := range map[string]string{"destination:ehr": "errored", "destination:archive": "active", "destination:tobilling": "idle"} {
		if e := edge(g, "transform:dsl:normalize", to); e.Kind != topology.EdgeMessagePath || e.Status != want {
			t.Errorf("edge to %s = %+v, want %s", to, e, want)
		}
	}
	if e := edge(g, "source:http", "transform:dsl:normalize"); e.Status != "active" || e.Activity.Received != 2 {
		t.Errorf("source edge = %+v", e)
	}
	if e := edge(g, "destination:tobilling", "flow:billing"); e.Kind != topology.EdgeRoute {
		t.Errorf("route = %+v", e)
	}
	if g2 := graph("/api/v1/topology/flows/adt"); len(g2.Nodes) != len(g.Nodes) {
		t.Errorf("the bare id gives %d nodes", len(g2.Nodes))
	}

	over := graph("/api/v1/topology")
	if n := node(over, "flow:billing"); n.Activity == nil || n.Activity.Received != 0 || n.Status != "started" {
		t.Errorf("billing = %+v", n)
	}
	if n := node(over, "flow:adt"); n.Activity.Received != 2 {
		t.Errorf("adt = %+v %+v", n, n.Activity)
	}
	edge(over, "flow:adt", "flow:billing")

	stop()
	if !restartable(t) {
		return
	}
	stop = startCLI(t, args, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	if n := node(graph("/api/v1/topology"), "flow:adt"); n.Activity == nil || n.Activity.Received != 2 {
		t.Errorf("adt after a restart = %+v", n)
	}
}
