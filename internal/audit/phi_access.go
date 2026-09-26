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

// Sensitive parameter names (spec §10). Long terms match as substrings of the
// key; the short terms "ssn" and "phi" only match as whole words, since as
// substrings they hit ordinary names such as "className" and "graphId".
var (
	sensitiveSubstrings = []string{"password", "token", "secret", "authorization", "credential"}
	sensitiveWords      = map[string]bool{"ssn": true, "phi": true}
)

// isSensitive reports whether key names a sensitive parameter, ignoring case.
// Words are split at non-alphanumerics and lower-to-upper case changes.
func isSensitive(key string) bool {
	lower := strings.ToLower(key)
	for _, s := range sensitiveSubstrings {
		if strings.Contains(lower, s) {
			return true
		}
	}
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
		if sensitiveWords[strings.ToLower(w)] {
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
