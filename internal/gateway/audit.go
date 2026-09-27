package gateway

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// AuditEvent is one audit record handed to the AuditSink port.
type AuditEvent struct {
	Actor    string
	Action   string
	Resource string
	// Detail carries the HTTP status and query parameters. The sink redacts
	// sensitive keys.
	Detail map[string]string
}

// Audit actions that are not "<METHOD> <route>". They match the audit
// package's action names (checked by the composition-root tests).
const (
	AuditLogin       = "auth.login"
	AuditAuthFailure = "auth.failure"
	AuditPHIAccess   = "phi.access"
)

// auditInfo is filled in while a request is handled and read by audited
// once the response is written.
type auditInfo struct {
	action    string            // set by auditAs on tagged routes
	attempted string            // username offered in credentials
	id        Identity          // set once authenticated
	detail    map[string]string // added by handlers (config apply)
}

type auditKey struct{}

func auditInfoFrom(ctx context.Context) *auditInfo {
	info, _ := ctx.Value(auditKey{}).(*auditInfo)
	if info == nil {
		return &auditInfo{} // unaudited request: writes are discarded
	}
	return info
}

// statusRecorder captures the response status for the audit entry.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// auditAs tags a route with a fixed audit action (login, PHI access). It must
// run before any middleware that can reject the request.
func (s *Server) auditAs(action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auditInfoFrom(r.Context()).action = action
			next.ServeHTTP(w, r)
		})
	}
}

// audited records, after the response is written: tagged routes (login, PHI
// access); every 401 as auth.failure; and every other non-GET request as
// "<METHOD> <route>", including rejected ones.
func (s *Server) audited(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := &auditInfo{}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), auditKey{}, info)))
		if s.cfg.Audit == nil {
			return
		}
		action := info.action
		if rec.status == http.StatusUnauthorized && action != AuditLogin {
			action = AuditAuthFailure
		}
		if action == "" {
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				return
			}
			// Requests rejected before routing (e.g. missing CSRF marker) only
			// have the wildcard pattern; record their literal path instead.
			route := requestedPath(r)
			if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" && !strings.HasSuffix(rc.RoutePattern(), "*") {
				route = rc.RoutePattern()
				if requestedPath(r) != r.URL.Path { // an unversioned call: keep its form
					route = strings.Replace(route, "/api/"+APIVersion+"/", "/api/", 1)
				}
			}
			action = r.Method + " " + route
		}
		actor := info.id.Username
		if actor == "" {
			actor = info.attempted
		}
		detail := map[string]string{"status": strconv.Itoa(rec.status)}
		for k, v := range r.URL.Query() {
			detail["query."+k] = strings.Join(v, ",")
		}
		for k, v := range info.detail {
			detail[k] = v
		}
		// Audit failures never change the response.
		_ = s.cfg.Audit.Record(r.Context(), AuditEvent{Actor: actor, Action: action, Resource: requestedPath(r), Detail: detail})
	})
}
