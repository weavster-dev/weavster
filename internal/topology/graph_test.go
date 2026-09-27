package topology

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestNewGraphEmptyLists: every graph builder starts from NewGraph, whose
// node and edge lists are empty rather than nil, so they serialize as [].
func TestNewGraphEmptyLists(t *testing.T) {
	for name, g := range map[string]Graph{"new": NewGraph(), "overview": Overview(nil), "flow": FlowInternal(FlowDetail{})} {
		if g.Edges == nil || len(g.Edges) != 0 || g.Nodes == nil {
			t.Errorf("%s: nodes %v, edges %v", name, g.Nodes, g.Edges)
		}
		b, err := json.Marshal(g)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), `"edges":[]`) {
			t.Errorf("%s = %s", name, b)
		}
	}
	if b, _ := json.Marshal(NewGraph()); !strings.Contains(string(b), `"nodes":[]`) {
		t.Errorf("new graph = %s", b)
	}
}
