package topology

// FlowSummary is the input for the overview graph.
type FlowSummary struct {
	ID       string
	Name     string
	Status   string
	Activity *Activity
	Routes   []string // destination flow ids (inter-flow routes)
	Deps     []string // deployment dependency flow ids
}

// Overview builds the overview graph: flow nodes with route/dependency edges
// (contract §3.1). No server-side layout is produced.
func Overview(flows []FlowSummary) Graph {
	g := NewGraph()
	for _, f := range flows {
		node := Node{ID: "flow:" + f.ID, Kind: KindFlow, Label: f.Name, Status: f.Status, Activity: f.Activity}
		g.Nodes = append(g.Nodes, node)
		for _, to := range f.Routes {
			g.Edges = append(g.Edges, Edge{
				ID:     "edge:flow:" + f.ID + ":route:flow:" + to,
				From:   "flow:" + f.ID,
				To:     "flow:" + to,
				Kind:   EdgeRoute,
				Label:  "routeMessage('" + to + "')",
				Status: "active",
			})
		}
		for _, dep := range f.Deps {
			g.Edges = append(g.Edges, Edge{
				ID:   "edge:flow:" + f.ID + ":dependency:flow:" + dep,
				From: "flow:" + f.ID,
				To:   "flow:" + dep,
				Kind: EdgeDependency,
			})
		}
	}
	return g
}
