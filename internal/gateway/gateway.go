// Package gateway implements the Weavster API Gateway: the REST+OpenAPI HTTP
// surface with authentication, authorization, audit, and transport hardening.
package gateway

import (
	"context"

	"github.com/weavster-dev/weavster/internal/observability"
	"github.com/weavster-dev/weavster/internal/topology"
)

// Identity is a minimal authenticated principal. The gateway defines its own
// types and depends only on ports, never on auth's concrete types (hexagonal).
type Identity struct {
	Username    string
	Permissions []string
}

// AuthProvider authenticates credentials (arch §3.1).
type AuthProvider interface {
	Authenticate(ctx context.Context, username, password, mfaCode string) (Identity, error)
}

// Authorizer checks resource/action permissions (arch §3.1).
type Authorizer interface {
	Authorize(ctx context.Context, id Identity, resource, action string) bool
}

// AuditSink records audit entries (arch §3.1).
type AuditSink interface {
	Record(ctx context.Context, actor, action, resource string) error
}

// Flow is a minimal flow record exposed over REST.
type Flow struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	SourceType string `json:"sourceType"`
	Status     string `json:"status"`
	Enabled    bool   `json:"enabled"`
}

// FlowStore is the flow CRUD backend.
type FlowStore interface {
	List(ctx context.Context) ([]Flow, error)
	Get(ctx context.Context, id string) (Flow, error)
	Create(ctx context.Context, f Flow) error
	Delete(ctx context.Context, id string) error
}

// Message is a minimal stored message exposed over REST.
type Message struct {
	ID          string `json:"id"`
	FlowID      string `json:"flowId"`
	Status      string `json:"status"`
	ContentType string `json:"contentType"`
}

// MessageQuery narrows a message search.
type MessageQuery struct {
	Status string
	FlowID string
	Limit  int
}

// MessageSearcher is the message search backend.
type MessageSearcher interface {
	Search(ctx context.Context, q MessageQuery) ([]Message, error)
}

// TopologyProvider serves the read-only topology graphs (contract §3).
type TopologyProvider interface {
	Overview(ctx context.Context) (topology.Graph, error)
	FlowInternal(ctx context.Context, id string) (topology.Graph, error)
}

// Config wires the gateway's ports.
type Config struct {
	Auth        AuthProvider
	Authorizer  Authorizer
	Audit       AuditSink
	Flows       FlowStore
	Messages    MessageSearcher
	Topology    TopologyProvider
	System      observability.SystemInfo
	RequireCSRF bool
}

// Server is the HTTP gateway.
type Server struct {
	cfg Config
}

// New returns a gateway server.
func New(cfg Config) *Server {
	return &Server{cfg: cfg}
}
