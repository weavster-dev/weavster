package auth

import "context"

// Resource-category permission constants (spec §2.8.27).
const (
	PermAdmin         = "admin"
	PermFlowsView     = "flows:view"
	PermFlowsEdit     = "flows:edit"
	PermMessagesView  = "messages:view"
	PermAlertsEdit    = "alerts:edit"
	PermEventsView    = "events:view"
	PermSnippetsEdit  = "snippets:edit"
	PermScriptsEdit   = "scripts:edit"
	PermConfigMapEdit = "configmap:edit"
	PermSettingsEdit  = "settings:edit"
)

// Authorizer is the port for authorization (arch §3.1).
type Authorizer interface {
	Authorize(ctx context.Context, user *User, resource, action string) bool
}

// LocalAuthorizer enforces the built-in resource-category permission set
// (spec §2.8.27). A user with the "admin" permission is granted everything.
type LocalAuthorizer struct{}

// NewLocalAuthorizer returns the built-in authorizer.
func NewLocalAuthorizer() *LocalAuthorizer { return &LocalAuthorizer{} }

func (LocalAuthorizer) Authorize(_ context.Context, user *User, resource, action string) bool {
	if user == nil {
		return false
	}
	need := resource + ":" + action
	for _, p := range user.Permissions {
		if p == PermAdmin || p == need {
			return true
		}
	}
	return false
}

var _ Authorizer = (*LocalAuthorizer)(nil)
