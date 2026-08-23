package topology

// Connector describes a source or destination in a flow.
type Connector struct {
	ID       string
	Label    string
	Type     string
	DataType string
	Status   string
}

// Stage describes a transform stage in a flow.
type Stage struct {
	ID     string
	Label  string
	Status string
}

// FlowDetail is the input for the flow-internal graph.
type FlowDetail struct {
	ID           string
	Name         string
	Status       string
	Sources      []Connector
	Transforms   []Stage
	Destinations []Connector
	Routes       []string // outbound route targets (flow ids)
}

// FlowInternal builds the flow-internal graph: source -> transform ->
// destination nodes with message-path edges, plus outbound route edges
// (contract §3.2).
func FlowInternal(f FlowDetail) Graph {
	g := NewGraph()
	g.FlowID = f.ID
	g.FlowName = f.Name
	g.FlowStatus = f.Status

	for _, s := range f.Sources {
		g.Nodes = append(g.Nodes, Node{
			ID: "source:" + s.ID, Kind: KindSource, Label: s.Label, Status: s.Status,
			Meta: map[string]string{"connectorType": s.Type, "dataType": s.DataType},
		})
	}
	for _, t := range f.Transforms {
		g.Nodes = append(g.Nodes, Node{ID: "transform:" + t.ID, Kind: KindTransform, Label: t.Label, Status: t.Status})
	}
	for _, d := range f.Destinations {
		g.Nodes = append(g.Nodes, Node{
			ID: "destination:" + d.ID, Kind: KindDestination, Label: d.Label, Status: d.Status,
			Meta: map[string]string{"connectorType": d.Type},
		})
	}

	// Message path: source -> first transform -> ... -> destination(s).
	if len(f.Sources) > 0 {
		first := "source:" + f.Sources[0].ID
		if len(f.Transforms) > 0 {
			next := "transform:" + f.Transforms[0].ID
			g.Edges = append(g.Edges, Edge{ID: "edge:" + first + ":path:" + next, From: first, To: next, Kind: EdgeMessagePath, Status: "active"})
			prev := next
			for _, t := range f.Transforms[1:] {
				cur := "transform:" + t.ID
				g.Edges = append(g.Edges, Edge{ID: "edge:" + prev + ":path:" + cur, From: prev, To: cur, Kind: EdgeMessagePath, Status: "active"})
				prev = cur
			}
			for _, d := range f.Destinations {
				to := "destination:" + d.ID
				g.Edges = append(g.Edges, Edge{ID: "edge:" + prev + ":path:" + to, From: prev, To: to, Kind: EdgeMessagePath, Status: "active"})
			}
		} else {
			for _, d := range f.Destinations {
				to := "destination:" + d.ID
				g.Edges = append(g.Edges, Edge{ID: "edge:" + first + ":path:" + to, From: first, To: to, Kind: EdgeMessagePath, Status: "active"})
			}
		}
	}

	// Outbound route edges.
	for _, to := range f.Routes {
		g.Edges = append(g.Edges, Edge{
			ID: "edge:flow:" + f.ID + ":route:flow:" + to, From: "flow:" + f.ID, To: "flow:" + to, Kind: EdgeRoute,
		})
	}
	return g
}
