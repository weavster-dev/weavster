package audit

import (
	"context"
	"strings"
	"unicode"
)

// Action constants for audit log entries (spec §10).
const (
	ActionPHIAccess = "phi.access"
	ActionPHIQuery  = "phi.query"
	ActionLogin     = "auth.login"
	ActionConfig    = "config.apply"
)

// sensitiveKeys are parameters excluded from audit capture (spec §10).
var sensitiveKeys = map[string]bool{
	"password": true, "token": true, "secret": true, "authorization": true,
	"credential": true, "ssn": true, "phi": true,
}

// isSensitive reports whether any word of key is sensitive, ignoring case.
// Words are split at non-alphanumerics and lower-to-upper case changes, so
// "newPassword", "X-Token", and "client_secret" match but "className" does
// not.
func isSensitive(key string) bool {
	var words []string
	start := 0
	runes := []rune(key)
	for i, c := range runes {
		boundary := !unicode.IsLetter(c) && !unicode.IsDigit(c)
		camel := i > 0 && unicode.IsUpper(c) && unicode.IsLower(runes[i-1])
		if boundary || camel {
			words = append(words, string(runes[start:i]))
			start = i
			if boundary {
				start = i + 1
			}
		}
	}
	words = append(words, string(runes[start:]))
	for _, w := range words {
		if sensitiveKeys[strings.ToLower(w)] {
			return true
		}
	}
	return false
}

// RedactSensitive returns a copy of detail with sensitive values redacted.
// Any word of a key (see isSensitive) matching a sensitive name, in any
// case, redacts the value.
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
