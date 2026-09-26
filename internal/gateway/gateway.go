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
	Status     string `json:"status,omitempty"`
	Enabled    bool   `json:"enabled"`
	// DependsOn lists flows this flow requires (ids); kept acyclic.
	DependsOn []string `json:"dependsOn,omitempty"`
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

// FlowUpdater changes a stored flow's definition; the runtime status is
// never changed by these calls.
type FlowUpdater interface {
	// Update replaces the definition; with keepEnabled the stored enabled
	// flag is kept instead of f.Enabled.
	Update(ctx context.Context, id string, f Flow, keepEnabled bool) (Flow, error)
	SetEnabled(ctx context.Context, id string, enabled bool) (Flow, error)
}

// FlowBundle is an export/import document.
type FlowBundle struct {
	Version int    `json:"version"`
	Flows   []Flow `json:"flows"`
}

// ImportResult lists the flows an import created and updated.
type ImportResult struct {
	Created []string `json:"created"`
	Updated []string `json:"updated"`
}

// FlowTransfer exports and imports flow definitions.
type FlowTransfer interface {
	// Export returns the selected flows (all when ids is empty) plus their
	// transitive dependencies, without status.
	Export(ctx context.Context, ids []string) ([]Flow, error)
	// Import validates every flow first, then writes them. Existing flows
	// are only replaced when overwrite is set (ErrImportConflict otherwise).
	Import(ctx context.Context, flows []Flow, overwrite bool) (ImportResult, error)
}

// FlowLifecycle changes a flow's runtime state (spec §6.1).
type FlowLifecycle interface {
	Transition(ctx context.Context, id, action string) (Flow, error)
	RedeployAll(ctx context.Context) ([]Flow, error)
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
	// ErrInvalidTransition is wrapped with the reason, e.g. "cannot pause a
	// flow that is stopped".
	ErrInvalidTransition = errors.New("invalid lifecycle transition")
	ErrUnknownAction     = errors.New("unknown lifecycle action")
	// ErrFlowInUse: another flow depends on this one.
	ErrFlowInUse = errors.New("flow is a dependency of other flows")
	// ErrImportConflict: the bundle contains flows that already exist.
	ErrImportConflict = errors.New("flows already exist")
	// ErrImportIncomplete: writing stopped part-way; the ImportResult lists
	// what was written.
	ErrImportIncomplete = errors.New("import stopped part-way")
)

// FlowStore is the flow CRUD backend.
type FlowStore interface {
	List(ctx context.Context) ([]Flow, error)
	Get(ctx context.Context, id string) (Flow, error)
	// Create stores a new flow and returns it as stored (with server-set
	// fields such as status).
	Create(ctx context.Context, f Flow) (Flow, error)
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
	Lifecycle   FlowLifecycle
	FlowUpdates FlowUpdater
	Transfer    FlowTransfer
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
