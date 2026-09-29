package topology

// FlowSummary is the input for the overview graph.
type FlowSummary struct {
	ID       string
	Name     string
	Status   string
	Activity *Activity
	Routes   []Link   // inter-flow routes, by target flow
	Deps     []string // deployment dependency flow ids
}

// Link is a route to another flow with the traffic across it.
type Link struct {
	Flow     string
	Status   string
	Activity *Activity
}

// Overview builds the overview graph: flow nodes with route/dependency edges
// (contract §3.1). No server-side layout is produced.
func Overview(flows []FlowSummary) Graph {
	g := NewGraph()
	for _, f := range flows {
		node := Node{ID: "flow:" + f.ID, Kind: KindFlow, Label: f.Name, Status: f.Status, Activity: f.Activity}
		g.Nodes = append(g.Nodes, node)
		for _, r := range f.Routes {
			g.Edges = append(g.Edges, Edge{
				ID:       "edge:flow:" + f.ID + ":route:flow:" + r.Flow,
				From:     "flow:" + f.ID,
				To:       "flow:" + r.Flow,
				Kind:     EdgeRoute,
				Label:    "routeMessage('" + r.Flow + "')",
				Status:   r.Status,
				Activity: r.Activity,
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
