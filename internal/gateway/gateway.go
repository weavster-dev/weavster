// Package gateway implements the Weavster API Gateway: the REST+OpenAPI HTTP
// surface with authentication, authorization, audit, and transport hardening.
package gateway

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/weavster-dev/weavster/internal/observability"
	"github.com/weavster-dev/weavster/internal/topology"
)

// Identity is a minimal authenticated principal. The gateway defines its own
// types and depends only on ports, never on auth's concrete types (hexagonal).
type Identity struct {
	Username           string
	Permissions        []string
	MustChangePassword bool
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
	Record(ctx context.Context, e AuditEvent) error
}

// Flow is a minimal flow record exposed over REST.
type Flow struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	SourceType string `json:"sourceType"`
	Status     string `json:"status"`
	Enabled    bool   `json:"enabled"`
	// Transform is the flow's YAML DSL transform as a JSON object (the
	// transform.schema.json shape); the gateway passes it through unparsed.
	Transform    json.RawMessage   `json:"transform,omitempty"`
	Destinations []FlowDestination `json:"destinations,omitempty"`
}

// FlowDestination is one delivery target of a flow.
type FlowDestination struct {
	Name string `json:"name"`
	Type string `json:"type"`
	URL  string `json:"url,omitempty"`
	Dir  string `json:"dir,omitempty"`
}

// IngestResult is the outcome of processing one received message.
type IngestResult struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// MessageIngester runs a received message through a flow.
type MessageIngester interface {
	Ingest(ctx context.Context, flowID string, body []byte) (IngestResult, error)
}

// Flow errors the ports return; handlers map them to 404, 409, and 400.
// ErrInvalidFlow and ErrInvalidMessage are wrapped with a message that is
// safe to show the client.
var (
	ErrFlowNotFound   = errors.New("flow not found")
	ErrFlowExists     = errors.New("flow already exists")
	ErrInvalidFlow    = errors.New("invalid flow")
	ErrInvalidMessage = errors.New("invalid message")
	ErrFlowNotRunning = errors.New("flow is not accepting messages")
)

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
	Auth        AuthProvider // nil disables authentication and authorization
	Passwords   PasswordChanger
	Authorizer  Authorizer
	Audit       AuditSink
	Flows       FlowStore
	Messages    MessageSearcher
	Ingest      MessageIngester
	Stats       StatsProvider
	Events      EventSearcher
	Topology    TopologyProvider
	System      observability.SystemInfo
	RequireCSRF bool
}

// Server is the HTTP gateway.
type Server struct {
	cfg      Config
	sessions *sessions
}

// New returns a gateway server.
func New(cfg Config) *Server {
	return &Server{cfg: cfg, sessions: newSessions()}
}
