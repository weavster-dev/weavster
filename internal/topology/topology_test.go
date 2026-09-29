package topology

import (
	"reflect"
	"strings"
	"testing"
)

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
	act := &Activity{Sent: 2}
	g := FlowInternal(FlowDetail{
		ID: "a", Name: "Patient Admit", Status: "started",
		Source:    &Part{ID: "file", Label: "file:/incoming", Status: "started", Meta: map[string]string{"connectorType": "file"}, EdgeStatus: "active"},
		Transform: &Part{ID: "dsl:normalize", Label: "normalize", Status: "started"},
		Destinations: []Part{
			{ID: "his", Label: "his", Status: "started", Activity: act, EdgeStatus: "errored"},
			{ID: "billing", Label: "billing", Status: "stopped", EdgeStatus: "idle"},
		},
		Routes: []Route{{Destination: "billing", Flow: "b"}},
	})
	if g.FlowID != "flow:a" || g.FlowName != "Patient Admit" || g.FlowStatus != "started" {
		t.Errorf("flow = %q %q %q", g.FlowID, g.FlowName, g.FlowStatus)
	}
	var ids []string
	for _, n := range g.Nodes {
		ids = append(ids, n.ID)
	}
	if strings.Join(ids, " ") != "source:file transform:dsl:normalize destination:his destination:billing" {
		t.Errorf("nodes = %v", ids)
	}
	want := []Edge{
		{ID: "edge:source:file:path:transform:dsl:normalize", From: "source:file", To: "transform:dsl:normalize", Kind: EdgeMessagePath, Status: "active"},
		{ID: "edge:transform:dsl:normalize:path:destination:his", From: "transform:dsl:normalize", To: "destination:his", Kind: EdgeMessagePath, Status: "errored", Activity: act},
		{ID: "edge:transform:dsl:normalize:path:destination:billing", From: "transform:dsl:normalize", To: "destination:billing", Kind: EdgeMessagePath, Status: "idle"},
		{ID: "edge:destination:billing:route:flow:b", From: "destination:billing", To: "flow:b", Kind: EdgeRoute, Label: "routeMessage('b')"},
	}
	if !reflect.DeepEqual(g.Edges, want) {
		t.Errorf("edges = %+v", g.Edges)
	}
}

// TestFlowInternalPaths: the message path starts at the first part the
// flow has.
func TestFlowInternalPaths(t *testing.T) {
	for _, tt := range []struct {
		name  string
		f     FlowDetail
		edges []string
	}{
		{"no source", FlowDetail{ID: "a", Transform: &Part{ID: "dsl:t"}, Destinations: []Part{{ID: "d"}}}, []string{"transform:dsl:t>destination:d"}},
		{"no transform", FlowDetail{ID: "a", Source: &Part{ID: "http"}, Destinations: []Part{{ID: "d"}}}, []string{"source:http>destination:d"}},
		{"destinations only", FlowDetail{ID: "a", Destinations: []Part{{ID: "d"}}}, nil},
		{"source only", FlowDetail{ID: "a", Source: &Part{ID: "http"}}, nil},
	} {
		var got []string
		for _, e := range FlowInternal(tt.f).Edges {
			got = append(got, e.From+">"+e.To)
		}
		if !reflect.DeepEqual(got, tt.edges) {
			t.Errorf("%s: edges %v, want %v", tt.name, got, tt.edges)
		}
	}
}
