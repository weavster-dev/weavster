// Package topology implements the read-only flow topology graph data contract.
package topology

import "time"

// Activity is the rolling traffic snapshot (contract §4).
type Activity struct {
	Received      int64  `json:"received,omitempty"`
	Sent          int64  `json:"sent,omitempty"`
	Errored       int64  `json:"errored,omitempty"`
	Queued        int64  `json:"queued,omitempty"`
	LastMessageAt string `json:"lastMessageAt,omitempty"`
}

// Node is a graph node (contract §2.1).
type Node struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`
	Label    string            `json:"label"`
	Status   string            `json:"status,omitempty"`
	Activity *Activity         `json:"activity,omitempty"`
	Meta     map[string]string `json:"meta,omitempty"`
}

// Edge is a graph edge (contract §2.2).
type Edge struct {
	ID       string    `json:"id"`
	From     string    `json:"from"`
	To       string    `json:"to"`
	Kind     string    `json:"kind"`
	Label    string    `json:"label,omitempty"`
	Status   string    `json:"status,omitempty"`
	Activity *Activity `json:"activity,omitempty"`
}

// Graph is the top-level read-only graph payload (contract §3).
type Graph struct {
	SchemaVersion string    `json:"schemaVersion"`
	GeneratedAt   time.Time `json:"generatedAt"`
	FlowID        string    `json:"flowId,omitempty"`
	FlowName      string    `json:"flowName,omitempty"`
	FlowStatus    string    `json:"flowStatus,omitempty"`
	Nodes         []Node    `json:"nodes"`
	Edges         []Edge    `json:"edges"`
}

// NewGraph returns a graph initialized with the schema version and timestamp.
func NewGraph() Graph {
	return Graph{SchemaVersion: "1", GeneratedAt: time.Now().UTC()}
}

// Node kinds and edge kinds (contract §2).
const (
	KindFlow        = "flow"
	KindSource      = "source"
	KindTransform   = "transform"
	KindDestination = "destination"

	EdgeRoute       = "route"
	EdgeMessagePath = "message-path"
	EdgeDependency  = "dependency"
)
