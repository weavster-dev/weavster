package audit

import (
	"context"
	"strings"
)

// Action constants for audit log entries (spec §10).
const (
	ActionPHIAccess = "phi.access"
	ActionPHIQuery  = "phi.query"
	ActionLogin     = "auth.login"
	ActionConfig    = "config.apply"
)

// sensitiveKeys are parameters excluded from audit capture (spec §10). A key
// is sensitive when it contains one of these, ignoring case.
var sensitiveKeys = []string{"password", "token", "secret", "authorization", "credential", "ssn", "phi"}

func isSensitive(key string) bool {
	k := strings.ToLower(key)
	for _, s := range sensitiveKeys {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// RedactSensitive returns a copy of detail with sensitive values redacted.
// Keys match case-insensitively and by substring (e.g. "newPassword").
func RedactSensitive(detail map[string]string) map[string]string {
	out := make(map[string]string, len(detail))
	for k, v := range detail {
		if isSensitive(k) {
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
