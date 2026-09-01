package gateway

import (
	"context"
	"net/http"
)

// MarkerHeader is the cross-site-request marker header required on API calls
// (spec §2.13.45, §10). Its value must equal MarkerValue.
const (
	MarkerHeader = "X-Weavster-CSRF"
	MarkerValue  = "1"
)

// SecurityHeaders emits transport-hardening headers: HSTS, clickjacking
// (X-Frame-Options DENY + CSP frame-ancestors 'none'), and content-type
// sniffing protection (spec §10).
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

// RequireMarkerHeader enforces the CSRF marker header, returning HTTP 400
// when it is absent or wrong (spec §2.13.45).
func RequireMarkerHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(MarkerHeader) != MarkerValue {
			http.Error(w, "missing cross-site-request marker header", http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// BlockTrace rejects TRACE/TRACK with HTTP 405 (spec §10).
func BlockTrace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodTrace || r.Method == "TRACK" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// contextKey is an unexported type for request-context keys.
type contextKey string

const identityCtxKey contextKey = "identity"

// IdentityFromContext returns the authenticated Identity stored in ctx, if any.
func IdentityFromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(identityCtxKey).(Identity)
	return id, ok
}

// Authenticate is HTTP middleware that enforces HTTP Basic Authentication
// (spec §3.1). If cfg.Auth is nil, authentication is skipped (degraded mode).
// On success the Identity is stored in the request context; on failure a
// WWW-Authenticate challenge is emitted with HTTP 401.
func (s *Server) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Auth == nil {
			next.ServeHTTP(w, r)
			return
		}
		username, password, ok := r.BasicAuth()
		if !ok {
			s.audit(r.Context(), "", "authenticate", "api:missing")
			w.Header().Set("WWW-Authenticate", `Basic realm="weavster"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		id, err := s.cfg.Auth.Authenticate(r.Context(), username, password, "")
		if err != nil {
			s.audit(r.Context(), username, "authenticate", "api:failed")
			w.Header().Set("WWW-Authenticate", `Basic realm="weavster"`)
			http.Error(w, "authentication failed", http.StatusUnauthorized)
			return
		}
		s.audit(r.Context(), id.Username, "authenticate", "api:success")
		ctx := context.WithValue(r.Context(), identityCtxKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Authorize returns HTTP middleware that enforces the given resource+action
// permission (spec §3.1). The request must carry an authenticated Identity in
// context; otherwise HTTP 401 is returned. If the caller lacks the required
// permission, HTTP 403 is returned. When cfg.Authorizer is nil, authorization
// is skipped (degraded mode).
func (s *Server) Authorize(resource, action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s.cfg.Authorizer == nil {
				next.ServeHTTP(w, r)
				return
			}
			id, ok := IdentityFromContext(r.Context())
			if !ok {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
			if !s.cfg.Authorizer.Authorize(r.Context(), id, resource, action) {
				s.audit(r.Context(), id.Username, "authorize:"+resource+":"+action, r.URL.Path)
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// audit is a nil-safe helper that records an audit entry when an AuditSink is
// configured.
func (s *Server) audit(ctx context.Context, actor, action, resource string) {
	if s.cfg.Audit == nil {
		return
	}
	_ = s.cfg.Audit.Record(ctx, actor, action, resource)
}
