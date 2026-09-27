// Package gateway implements the Weavster API Gateway: the REST+OpenAPI HTTP
// surface with authentication, authorization, audit, and transport hardening.
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/weavster-dev/weavster/internal/flowdef"
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

// Flow is a flow definition with its runtime state (flowdef.Flow).
type Flow = flowdef.Flow

// FlowDestination is one delivery target of a flow (flowdef.Destination).
type FlowDestination = flowdef.Destination

// FlowUpdater changes a stored flow's definition; the runtime status is
// never changed by these calls.
type FlowUpdater interface {
	// Update replaces the definition; with keepEnabled the stored enabled
	// flag is kept instead of f.Enabled.
	Update(ctx context.Context, id string, f Flow, keepEnabled bool) (Flow, error)
	SetEnabled(ctx context.Context, id string, enabled bool) (Flow, error)
	// UpdateMany replaces several definitions. Every flow must exist and be
	// valid before any is written (ErrFlowNotFound, ErrInvalidFlow); on a
	// failed write (ErrUpdateIncomplete) it returns the ids already written.
	UpdateMany(ctx context.Context, changes []FlowChange) ([]string, error)
}

// FlowChange is one entry of a bulk update.
type FlowChange struct {
	Flow Flow
	// KeepEnabled keeps the stored enabled flag instead of Flow.Enabled.
	KeepEnabled bool
}

// PortInUse is a network port the server listens on.
type PortInUse struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
	// UsedBy names the listener, e.g. "api" or "api-tls".
	UsedBy string `json:"usedBy"`
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
	// TransitionAll applies action to every flow it applies to (spec §5
	// "all"); on a store failure (ErrTransitionIncomplete) the result lists
	// the flows already changed.
	TransitionAll(ctx context.Context, action string) (TransitionAllResult, error)
	// SetDestinationRunning starts (running) or stops one destination.
	SetDestinationRunning(ctx context.Context, id, destination string, running bool) (Flow, error)
}

// TransitionAllResult reports an all-flows lifecycle action.
type TransitionAllResult struct {
	Changed []string      `json:"changed"`
	Skipped []SkippedFlow `json:"skipped"`
}

// SkippedFlow is a flow an all-flows action left alone, and why.
type SkippedFlow struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// IngestResult is the outcome of processing one received message.
type IngestResult struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	// Response is the flow's selected destination reply, when there is one.
	Response json.RawMessage `json:"response,omitempty"`
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
	// ErrDependency: a flow's dependency is missing or cannot be deployed;
	// wrapped with the reason.
	ErrDependency = errors.New("dependency problem")
	// ErrDestinationNotFound: the flow has no destination with that name.
	ErrDestinationNotFound = errors.New("destination not found")
	// ErrFlowInUse: another flow depends on this one.
	ErrFlowInUse = errors.New("flow is a dependency of other flows")
	// ErrImportConflict: the bundle contains flows that already exist.
	ErrImportConflict = errors.New("flows already exist")
	// ErrImportIncomplete: writing stopped part-way; the ImportResult lists
	// what was written.
	ErrImportIncomplete = errors.New("import stopped part-way")
	// ErrTransitionIncomplete: an all-flows action stopped part-way.
	ErrTransitionIncomplete = errors.New("all-flows action stopped part-way")
	// ErrUpdateIncomplete: a bulk update stopped part-way.
	ErrUpdateIncomplete = errors.New("update stopped part-way")
	// ErrMessageNotFound: no stored message has the id.
	ErrMessageNotFound = errors.New("message not found")
	// ErrNoContent: the message has no content of the requested part (for
	// example no transformed content yet); wrapped with the detail.
	ErrNoContent = errors.New("no such content")
	// ErrInvalidArchive: an import is not a readable archive (not gzip,
	// wrong key, or not an export document); wrapped with the reason.
	ErrInvalidArchive = errors.New("invalid message archive")
	// ErrMessageImportIncomplete: a message import stopped part-way.
	ErrMessageImportIncomplete = errors.New("message import stopped part-way")
	// ErrMessageBusy: the message is being processed or retried.
	ErrMessageBusy = errors.New("message is being processed; try again")
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

// Message is a stored message without its content.
type Message struct {
	ID          string                    `json:"id"`
	FlowID      string                    `json:"flowId"`
	Status      string                    `json:"status"`
	ContentType string                    `json:"contentType"`
	ReceivedAt  time.Time                 `json:"receivedAt"`
	UpdatedAt   time.Time                 `json:"updatedAt"`
	Attempts    map[string]MessageAttempt `json:"attempts,omitempty"`
	Metadata    map[string]string         `json:"metadata,omitempty"`
}

// MessageAttempt is one destination's delivery state for a message.
type MessageAttempt struct {
	Attempts      int        `json:"attempts"`
	LastError     string     `json:"lastError,omitempty"`
	NextAttemptAt *time.Time `json:"nextAttemptAt,omitempty"`
}

// MessageQuery narrows a message search; the store applies every filter
// before Limit and Offset.
type MessageQuery struct {
	Status   string
	FlowID   string
	From, To time.Time // receive time, inclusive; zero = open
	Limit    int
	Offset   int
	Sort     string // receivedAt or id, "-" prefix for descending
}

// MessageContent is one stored part of a message.
type MessageContent struct {
	Body        []byte
	ContentType string // MIME type
}

// MessageStore reads and manages stored messages.
type MessageStore interface {
	Search(ctx context.Context, q MessageQuery) ([]Message, error)
	// Get returns one message (ErrMessageNotFound).
	Get(ctx context.Context, id string) (Message, error)
	// Content returns a part of a message: "raw" or "transformed".
	Content(ctx context.Context, id, part string) (MessageContent, error)
	// Delete removes a message (ErrMessageBusy while it is processed).
	Delete(ctx context.Context, id string) error
	// Reprocess runs the message's original content through its flow again
	// as a new message.
	Reprocess(ctx context.Context, id string) (IngestResult, error)
	// Export writes an archive of the matching messages (complete, every
	// content part), encrypted when key is set.
	Export(ctx context.Context, q MessageQuery, key []byte) (archive []byte, count int, err error)
	// Import restores an archive: ErrInvalidArchive when it cannot be read,
	// ErrFlowNotFound when it refers to a missing flow (nothing written),
	// ErrMessageImportIncomplete when a write fails part-way (the result
	// counts what was written).
	Import(ctx context.Context, archive []byte, opts MessageImport) (MessageImportResult, error)
	// DeleteMatching removes every message matching q's filters (its limit,
	// offset, and sort are ignored); messages being processed are skipped
	// and counted as busy.
	DeleteMatching(ctx context.Context, q MessageQuery) (deleted, busy int, err error)
}

// MessagesDeleted reports a bulk removal.
type MessagesDeleted struct {
	Deleted int `json:"deleted"`
	// Busy counts matches left alone because they were being processed.
	Busy int `json:"busy"`
	// Restarted lists the flows stopped for the removal and started again.
	Restarted []string `json:"restarted"`
}

// MessageImport controls an import: FlowID assigns every message to that
// flow; existing ids are skipped unless Overwrite; Key decrypts.
type MessageImport struct {
	FlowID    string
	Overwrite bool
	Key       []byte
}

// MessageImportResult counts an import.
type MessageImportResult struct {
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"` // the id exists and overwrite is off
	Busy     int `json:"busy"`    // being processed; left as it is
}

// TopologyProvider serves the read-only topology graphs (contract §3).
type TopologyProvider interface {
	Overview(ctx context.Context) (topology.Graph, error)
	FlowInternal(ctx context.Context, id string) (topology.Graph, error)
}

// Config wires the gateway's ports.
type Config struct {
	Auth      AuthProvider // nil disables authentication and authorization
	Passwords PasswordChanger
	Users     UserAdmin
	Items     ItemStore
	// ConfigPlanner plans config-as-code documents against the server.
	ConfigPlanner ConfigPlanner
	// ConfigValidator checks config-as-code documents.
	ConfigValidator ConfigValidator
	// Trends counts messages over time.
	Trends MessageTrendReader
	// Lookups keeps dynamic lookup groups.
	Lookups LookupStore
	// Alerts keeps alert definitions.
	Alerts AlertStore
	// Snippets keeps code snippets and snippet libraries.
	Snippets    SnippetStore
	Authorizer  Authorizer
	Audit       AuditSink
	Flows       FlowStore
	Messages    MessageStore
	Ingest      MessageIngester
	Lifecycle   FlowLifecycle
	FlowUpdates FlowUpdater
	Transfer    FlowTransfer
	Stats       StatsProvider
	// StatsHistory reads the sampled statistics time series.
	StatsHistory StatsHistory
	Events       EventSearcher
	Topology     TopologyProvider
	System       SystemReporter
	// Listeners are the ports the server listens on (ports-in-use).
	Listeners   []PortInUse
	RequireCSRF bool
}

// Server is the HTTP gateway.
type Server struct {
	cfg      Config
	sessions *sessions
	applyMu  sync.Mutex // one configuration apply at a time
}

// New returns a gateway server.
func New(cfg Config) *Server {
	return &Server{cfg: cfg, sessions: newSessions()}
}
