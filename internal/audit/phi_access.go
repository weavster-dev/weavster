package audit

import "context"

// Action constants for audit log entries (spec §10).
const (
	ActionPHIAccess = "phi.access"
	ActionPHIQuery  = "phi.query"
	ActionLogin     = "auth.login"
	ActionConfig    = "config.apply"
)

// sensitiveKeys are parameters excluded from audit capture (spec §10).
var sensitiveKeys = map[string]bool{
	"password":      true,
	"token":         true,
	"secret":        true,
	"authorization": true,
	"credential":    true,
	"ssn":           true,
	"phi":           true,
}

// RedactSensitive returns a copy of detail with sensitive values redacted.
func RedactSensitive(detail map[string]string) map[string]string {
	out := make(map[string]string, len(detail))
	for k, v := range detail {
		if sensitiveKeys[k] {
			out[k] = "[redacted]"
			continue
		}
		out[k] = v
	}
	return out
}

// RecordPHIAccess records a protected-content access event, excluding
// sensitive parameters (spec §10).
func RecordPHIAccess(ctx context.Context, sink AuditSink, actor, resource string, detail map[string]string) error {
	return sink.Record(ctx, Entry{
		Actor:    actor,
		Action:   ActionPHIAccess,
		Resource: resource,
		Detail:   RedactSensitive(detail),
	})
}
