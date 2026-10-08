package topology

// Part is a source, transform, or destination of a flow: its node, and the
// message-path edge that reaches it (for a destination) or leaves it (for
// the source).
type Part struct {
	ID       string // unique within the flow, without the kind prefix
	Label    string
	Status   string
	Activity *Activity
	Meta     map[string]string
	// EdgeStatus is the status of the part's message-path edge.
	EdgeStatus string
}

// Route is an outbound route from a destination to another flow, with the
// traffic across it.
type Route struct {
	Destination string // the destination's Part.ID
	Flow        string // the target flow id
	Status      string
	Activity    *Activity
}

// FlowDetail is the input for the flow-internal graph.
type FlowDetail struct {
	ID           string
	Name         string
	Status       string
	Source       *Part // nil: messages only arrive through the API
	Transform    *Part // nil: messages pass through unchanged
	Destinations []Part
	Routes       []Route
}

// FlowInternal builds the flow-internal graph (contract §3.2, #107 D-13):
// source -> transform -> each destination along message-path edges, and a
// route edge from each flow destination to its target flow, which is
// included as a flow node so every edge ends at a node.
func FlowInternal(f FlowDetail) Graph {
	g := NewGraph()
	if f.ID != "" {
		g.FlowID = "flow:" + f.ID
	}
	g.FlowName = f.Name
	g.FlowStatus = f.Status

	var prev *Edge // the edge template from the last node on the path
	if s := f.Source; s != nil {
		id := "source:" + s.ID
		g.Nodes = append(g.Nodes, Node{ID: id, Kind: KindSource, Label: s.Label, Status: s.Status, Activity: s.Activity, Meta: s.Meta})
		prev = &Edge{From: id, Status: s.EdgeStatus, Activity: s.Activity}
	}
	if t := f.Transform; t != nil {
		id := "transform:" + t.ID
		g.Nodes = append(g.Nodes, Node{ID: id, Kind: KindTransform, Label: t.Label, Status: t.Status, Activity: t.Activity, Meta: t.Meta})
		if prev != nil {
			g.Edges = append(g.Edges, pathEdge(prev.From, id, prev.Status, prev.Activity))
		}
		prev = &Edge{From: id}
	}
	for _, d := range f.Destinations {
		id := "destination:" + d.ID
		g.Nodes = append(g.Nodes, Node{ID: id, Kind: KindDestination, Label: d.Label, Status: d.Status, Activity: d.Activity, Meta: d.Meta})
		if prev != nil {
			g.Edges = append(g.Edges, pathEdge(prev.From, id, d.EdgeStatus, d.Activity))
		}
	}
	targets := map[string]bool{}
	for _, r := range f.Routes {
		from, to := "destination:"+r.Destination, "flow:"+r.Flow
		g.Edges = append(g.Edges, Edge{ID: "edge:" + from + ":route:" + to, From: from, To: to, Kind: EdgeRoute,
			Label: "routeMessage('" + r.Flow + "')", Status: r.Status, Activity: r.Activity})
		if !targets[r.Flow] {
			targets[r.Flow] = true
			g.Nodes = append(g.Nodes, Node{ID: to, Kind: KindFlow, Label: r.Flow})
		}
	}
	return g
}

func pathEdge(from, to, status string, activity *Activity) Edge {
	return Edge{ID: "edge:" + from + ":path:" + to, From: from, To: to, Kind: EdgeMessagePath, Status: status, Activity: activity}
}
