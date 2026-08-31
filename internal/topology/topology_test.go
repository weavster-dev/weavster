package topology

import "testing"

func TestOverview(t *testing.T) {
	g := Overview([]FlowSummary{
		{ID: "a", Name: "Patient Admit", Status: "started", Routes: []string{"b"}},
		{ID: "b", Name: "Billing", Status: "stopped"},
	})
	if g.SchemaVersion != "1" {
		t.Errorf("schemaVersion = %q", g.SchemaVersion)
	}
	if len(g.Nodes) != 2 || g.Nodes[0].ID != "flow:a" || g.Nodes[1].ID != "flow:b" {
		t.Errorf("nodes = %+v", g.Nodes)
	}
	if len(g.Edges) != 1 || g.Edges[0].Kind != EdgeRoute || g.Edges[0].From != "flow:a" || g.Edges[0].To != "flow:b" {
		t.Errorf("edges = %+v", g.Edges)
	}
}

func TestOverviewDependencyEdgeAndActivity(t *testing.T) {
	activity := &Activity{Received: 4, Sent: 3, Errored: 1, Queued: 2}
	g := Overview([]FlowSummary{{
		ID:       "orders",
		Name:     "Order Intake",
		Status:   "started",
		Activity: activity,
		Deps:     []string{"customers"},
	}})

	if len(g.Nodes) != 1 {
		t.Fatalf("node count = %d, want 1", len(g.Nodes))
	}
	if g.Nodes[0].Activity != activity {
		t.Errorf("node activity = %+v, want original activity pointer %+v", g.Nodes[0].Activity, activity)
	}
	if len(g.Edges) != 1 {
		t.Fatalf("edge count = %d, want 1", len(g.Edges))
	}
	edge := g.Edges[0]
	if edge.ID != "edge:flow:orders:dependency:flow:customers" ||
		edge.From != "flow:orders" ||
		edge.To != "flow:customers" ||
		edge.Kind != EdgeDependency {
		t.Errorf("dependency edge = %+v", edge)
	}
	if edge.Label != "" || edge.Status != "" {
		t.Errorf("dependency edge label/status = %q/%q, want empty", edge.Label, edge.Status)
	}
}

func TestFlowInternal(t *testing.T) {
	g := FlowInternal(FlowDetail{
		ID:     "a",
		Name:   "Patient Admit",
		Status: "started",
		Sources: []Connector{
			{ID: "file-1", Label: "file:///incoming", Type: "file", DataType: "hl7v2", Status: "started"},
		},
		Transforms: []Stage{
			{ID: "normalize", Label: "normalize", Status: "started"},
		},
		Destinations: []Connector{
			{ID: "mllp-1", Label: "HIS MLLP", Type: "tcp", Status: "started"},
		},
		Routes: []string{"b"},
	})
	if g.FlowID != "a" {
		t.Errorf("flowId = %q", g.FlowID)
	}
	// source -> transform -> destination message-path + route edge.
	if len(g.Edges) != 3 {
		t.Fatalf("edges = %+v", g.Edges)
	}
	if g.Edges[0].Kind != EdgeMessagePath || g.Edges[1].Kind != EdgeMessagePath || g.Edges[2].Kind != EdgeRoute {
		t.Errorf("edge kinds = %+v", g.Edges)
	}
	if g.Nodes[0].ID != "source:file-1" || g.Nodes[1].ID != "transform:normalize" || g.Nodes[2].ID != "destination:mllp-1" {
		t.Errorf("nodes = %+v", g.Nodes)
	}
}

func TestStableIDs(t *testing.T) {
	g1 := FlowInternal(FlowDetail{ID: "a", Name: "X", Sources: []Connector{{ID: "s1", Type: "file"}}, Destinations: []Connector{{ID: "d1", Type: "tcp"}}})
	g2 := FlowInternal(FlowDetail{ID: "a", Name: "X", Sources: []Connector{{ID: "s1", Type: "file"}}, Destinations: []Connector{{ID: "d1", Type: "tcp"}}})
	if len(g1.Nodes) != len(g2.Nodes) {
		t.Fatal("node counts differ")
	}
	for i := range g1.Nodes {
		if g1.Nodes[i].ID != g2.Nodes[i].ID {
			t.Errorf("node ids differ: %q vs %q", g1.Nodes[i].ID, g2.Nodes[i].ID)
		}
	}
}
