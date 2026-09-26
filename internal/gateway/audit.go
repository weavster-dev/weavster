package gateway

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// AuditEvent is one audit record handed to the AuditSink port.
type AuditEvent struct {
	Actor    string
	Action   string
	Resource string
	// Detail carries the HTTP status and query parameters. The sink is
	// responsible for redacting sensitive keys.
	Detail map[string]string
}

// Audit actions recorded outside the per-request middleware.
const (
	AuditLogin       = "auth.login"
	AuditAuthFailure = "auth.failure"
	AuditPHIAccess   = "phi.access"
)

// statusRecorder captures the response status for the audit entry.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// audited records administrative actions (every non-GET request) and
// protected-content reads (phi.access) after the handler runs, including
// requests that were rejected.
func (s *Server) audited(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if s.cfg.Audit == nil {
			return
		}
		action := r.Method + " " + chi.RouteContext(r.Context()).RoutePattern()
		switch {
		case r.Method != http.MethodGet && r.Method != http.MethodHead:
		case r.URL.Path == "/api/v1/messages":
			action = AuditPHIAccess
		default:
			return
		}
		id, _ := IdentityFrom(r.Context())
		s.record(r.Context(), id.Username, action, r.URL.Path, rec.status, r)
	})
}

// record sends one event to the AuditSink with the status and query
// parameters as detail. Audit failures never change the response.
func (s *Server) record(ctx context.Context, actor, action, resource string, status int, r *http.Request) {
	if s.cfg.Audit == nil {
		return
	}
	detail := map[string]string{"status": strconv.Itoa(status)}
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			detail["query."+k] = v[0]
		}
	}
	_ = s.cfg.Audit.Record(ctx, AuditEvent{Actor: actor, Action: action, Resource: resource, Detail: detail})
}
