package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
)

// APIVersion is the latest API version; unversioned /api/... paths use it
// (spec §5).
const APIVersion = "v1"

// apiVersion serves unversioned API paths (/api/flows) as the latest
// version (/api/v1/flows), like http.StripPrefix: on a copy of the request,
// with Path and RawPath rewritten alike. The path the client called stays
// available to the audit log (requestedPath). /api/openapi.yaml and bare
// /api/ are not versioned.
func apiVersion(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest, ok := strings.CutPrefix(r.URL.Path, "/api/")
		first, _, _ := strings.Cut(rest, "/")
		if !ok || rest == "" || rest == "openapi.yaml" || isVersion(first) {
			next.ServeHTTP(w, r)
			return
		}
		r2 := r.WithContext(context.WithValue(r.Context(), requestedPathKey{}, r.URL.Path))
		u := *r.URL
		u.Path = "/api/" + APIVersion + "/" + rest
		if raw, ok := strings.CutPrefix(u.RawPath, "/api/"); ok {
			u.RawPath = "/api/" + APIVersion + "/" + raw
		}
		r2.URL = &u
		next.ServeHTTP(w, r2)
	})
}

// requestedPathKey holds the path the client called before apiVersion
// rewrote it.
type requestedPathKey struct{}

// requestedPath is the path the client called.
func requestedPath(r *http.Request) string {
	if p, ok := r.Context().Value(requestedPathKey{}).(string); ok {
		return p
	}
	return r.URL.Path
}

// versionHeader names the API version that serves a route group.
func versionHeader(version string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Weavster-API-Version", version)
			next.ServeHTTP(w, r)
		})
	}
}

// isVersion reports whether a path segment names an API version (v1, v2, ...).
func isVersion(seg string) bool {
	if len(seg) < 2 || seg[0] != 'v' {
		return false
	}
	for _, c := range seg[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// RouteNotFoundMessage is the error message for a path no route serves.
const RouteNotFoundMessage = "no such endpoint"

// Router builds the chi router with middleware and routes.
func (s *Server) Router() http.Handler {
	return underContextPath(s.cfg.ContextPath, s.routes())
}

// underContextPath serves h under prefix (listen.contextPath, spec §4.1):
// /weavster/api/v1/flows reaches h as /api/v1/flows. Every other path
// reaches h as one no route matches, so it gets the router's own 404 with
// its security headers. An empty prefix serves h at the root.
func underContextPath(prefix string, h http.Handler) http.Handler {
	if prefix == "" {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest, ok := strings.CutPrefix(r.URL.Path, prefix)
		if !ok || (rest != "" && rest[0] != '/') {
			rest = "/outside-the-context-path" // matches no route
		}
		if rest == "" {
			rest = "/"
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path, r2.URL.RawPath = rest, rawAfter(r.URL.EscapedPath(), prefix, ok && rest != "/outside-the-context-path")
		h.ServeHTTP(w, r2)
	})
}

// rawAfter is the escaped path after the context prefix (which may itself
// be escaped), so escaped parameters such as a lookup key A%2FB keep their
// form; "" when the path is outside the prefix.
func rawAfter(escaped, prefix string, inside bool) string {
	if !inside {
		return ""
	}
	for i := len(prefix); i <= len(escaped); i++ {
		if (i == len(escaped) || escaped[i] == '/') && unescapes(escaped[:i], prefix) {
			return escaped[i:]
		}
	}
	return ""
}

func unescapes(escaped, want string) bool {
	got, err := url.PathUnescape(escaped)
	return err == nil && got == want
}

// routes is the API's router.
func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(apiVersion)
	r.Use(SecurityHeaders)
	r.Use(negotiateXML)
	// BlockTrace refuses TRACE before routing; say which methods the path
	// does allow (RFC 9110 requires Allow on 405).
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Method == http.MethodTrace || req.Method == "TRACK" {
				w.Header().Set("Allow", allowedMethods(r, req.URL.Path))
			}
			BlockTrace(next).ServeHTTP(w, req)
		})
	})
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeStatusError(w, http.StatusNotFound, RouteNotFoundMessage)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		// A custom handler replaces chi's, which set Allow (RFC 9110).
		w.Header().Set("Allow", allowedMethods(r, req.URL.Path))
		writeStatusError(w, http.StatusMethodNotAllowed, "method not allowed for this endpoint")
	})

	// Unauthenticated metadata.
	r.Get("/api/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte(OpenAPISpec()))
	})

	// Prometheus scrapes with credentials (basic_auth) but no CSRF marker:
	// outside /api/v1, audited and authenticated as it is.
	if s.cfg.Metrics != nil {
		r.With(s.audited, s.authenticate, s.require("flows", "view")).Get("/metrics", s.cfg.Metrics.ServeHTTP)
	}

	r.Route("/api/"+APIVersion, func(r chi.Router) {
		r.Use(versionHeader(APIVersion))
		// audited runs first so requests rejected by any later middleware
		// (CSRF marker, authentication, authorization) are still recorded.
		r.Use(s.audited)
		if s.cfg.RequireCSRF {
			r.Use(RequireMarkerHeader)
		}
		r.With(s.auditAs(AuditLogin)).Post("/auth/login", s.handleLogin)
		r.Group(func(r chi.Router) {
			r.Use(s.authenticate)
			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)
			r.Post("/auth/password", s.handleChangePassword)
			r.Post("/auth/password/check", s.handlePasswordCheck)
			r.Get("/users/{name}/preferences", s.handlePreferencesGet)
			r.Put("/users/{name}/preferences", s.handlePreferencesPut)
			r.Get("/users/{name}/loggedin", s.handleLoggedIn)
			r.Get("/system", s.systemRoute(func(sr SystemReporter) any { return sr.Status() }))
			r.Get("/system/about", s.systemRoute(func(sr SystemReporter) any { return sr.About() }))
			r.Get("/system/password-requirements", s.systemRoute(func(sr SystemReporter) any { return sr.PasswordRequirements() }))
			r.Get("/system/resources", s.systemRoute(func(sr SystemReporter) any { return sr.Resources() }))
			r.Get("/system/guid", s.handleGUID)
			r.With(s.require("flows", "view")).Get("/topology", s.handleTopologyOverview)
			r.With(s.require("flows", "view")).Get("/topology/flows/{flowId}", s.handleTopologyFlow)
			r.With(s.require("flows", "view")).Get("/flows", s.handleFlowsList)
			r.With(s.require("flows", "view")).Get("/flows/export", s.handleFlowsExport)
			r.With(s.require("flows", "view")).Get("/flows/connector-names", s.handleConnectorNames)
			r.With(s.require("flows", "view")).Get("/flows/ports-in-use", s.handlePortsInUse)
			r.With(s.require("flows", "edit")).Put("/flows", s.handleFlowsBulkUpdate)
			r.With(s.require("flows", "edit")).Post("/flows/import", s.handleFlowsImport)
			r.With(s.require("flows", "edit")).Post("/flows", s.handleFlowsCreate)
			r.With(s.require("flows", "view")).Get("/flows/{id}", s.handleFlowsGet)
			r.With(s.require("flows", "edit")).Delete("/flows/{id}", s.handleFlowsDelete)
			r.With(s.require("flows", "edit")).Put("/flows/{id}", s.handleFlowsUpdate)
			r.With(s.require("flows", "edit")).Post("/flows/{id}/enable", s.handleFlowEnable(true))
			r.With(s.require("flows", "edit")).Post("/flows/{id}/disable", s.handleFlowEnable(false))
			r.With(s.require("messages", "send")).Post("/flows/{id}/messages", s.handleIngest)
			r.With(s.require("flows", "deploy")).Post("/flows/redeploy-all", s.handleRedeployAll)
			for _, action := range AllFlowActions {
				r.With(s.require("flows", "deploy")).Post("/flows/"+action+"-all", s.handleActionAll(action))
			}
			r.With(s.require("flows", "view")).Get("/flows/stats", s.handleAllFlowStats)
			r.With(s.require("flows", "view")).Get("/stats/series", s.handleStatsSeries)
			r.With(s.require("flows", "deploy")).Post("/flows/stats/reset", s.handleResetStats)
			r.With(s.require("flows", "deploy")).Post("/flows/{id}/stats/reset", s.handleResetStats)
			r.With(s.require("flows", "deploy")).Post("/flows/{id}/{action}", s.handleFlowAction)
			r.With(s.require("flows", "deploy")).Post("/flows/{id}/destinations/{dest}/{action}", s.handleDestinationAction)
			r.With(s.require("flows", "view")).Get("/flows/{id}/stats", s.handleFlowStats)
			r.With(s.require("events", "view")).Get("/events", s.handleEvents)
			r.With(s.require("events", "view")).Get("/events/count", s.handleEventCount)
			r.With(s.require("events", "view")).Get("/events/max-id", s.handleEventMaxID)
			r.With(s.require("events", "view")).Get("/events/export", s.handleEventExport)
			r.With(s.require("events", "view")).Get("/events/{id}", s.handleEventGet)
			r.With(s.auditAs(AuditPHIAccess), s.require("messages", "view")).Get("/messages", s.handleMessagesSearch)
			for _, k := range itemKinds {
				edit := s.require(k.resource, "edit")
				r.With(edit).Get("/"+k.kind, s.handleItemsList(k))
				r.With(edit).Put("/"+k.kind, s.handleItemsReplace(k))
				r.With(edit).Get("/"+k.kind+"/{name}", s.handleItemGet(k))
				r.With(edit).Put("/"+k.kind+"/{name}", s.handleItemPut(k))
				r.With(edit).Delete("/"+k.kind+"/{name}", s.handleItemDelete(k))
			}
			// Validation reads nothing from the server, but parsing up to
			// 50 MiB is work: it needs flows:edit like the other large bodies.
			r.With(s.require("flows", "edit")).Post("/config/validate", s.handleConfigValidate)
			// Apply writes every kind of configuration.
			r.With(s.require("flows", "view"), s.require("flows", "edit"), s.require("alerts", "edit"), s.require("snippets", "edit"),
				s.require("scripts", "edit"), s.require("settings", "edit"), s.require("configmap", "edit")).Post("/config/apply", s.handleConfigApply)
			// A plan reads the whole live configuration: the export permissions.
			r.With(s.require("flows", "view"), s.require("alerts", "edit"), s.require("snippets", "edit"),
				s.require("scripts", "edit"), s.require("settings", "edit"), s.require("configmap", "edit")).Post("/config/plan", s.handleConfigPlan)
			// The config map and deploying need their permissions only when
			// the request touches them.
			r.With(s.require("flows", "view"), s.require("alerts", "edit"), s.require("snippets", "edit"),
				s.require("scripts", "edit"), s.require("settings", "edit"),
				s.requireWhen(queryTrue("includeConfigMap"), "configmap", "edit")).Get("/config/export", s.handleConfigExport)
			r.With(s.require("flows", "edit"), s.require("alerts", "edit"), s.require("snippets", "edit"),
				s.require("scripts", "edit"), s.require("settings", "edit"),
				s.requireWhen(func(r *http.Request) bool { return !queryTrue("nodeploy")(r) }, "flows", "deploy"),
				s.requireWhen(queryTrue("overwriteConfigMap"), "configmap", "edit")).Post("/config/import", s.handleConfigImport)
			lookupsView, lookupsEdit := s.require("lookups", "view"), s.require("lookups", "edit")
			r.With(lookupsView).Get("/lookups", s.handleLookupGroups)
			r.With(lookupsView).Get("/lookups/{group}", s.handleLookupMatching)
			r.With(lookupsEdit).Delete("/lookups/{group}", s.handleLookupDeleteGroup)
			r.With(lookupsEdit).Post("/lookups/{group}/import", s.handleLookupImport)
			r.With(lookupsView).Post("/lookups/{group}/batch", s.handleLookupBatch)
			r.With(lookupsView).Get("/lookups/{group}/{key}", s.handleLookupGet)
			r.With(lookupsView).Get("/lookups/{group}/{key}/exists", s.handleLookupExists)
			r.With(lookupsEdit).Put("/lookups/{group}/{key}", s.handleLookupPut)
			r.With(lookupsEdit).Delete("/lookups/{group}/{key}", s.handleLookupDelete)
			alerts := s.require("alerts", "edit")
			r.With(alerts).Get("/alerts", s.handleAlertsList)
			r.With(alerts).Post("/alerts", s.handleAlertsSave)
			r.With(alerts).Post("/alerts/import", s.handleAlertsImport)
			r.With(alerts).Get("/alerts/options", s.handleAlertOptions)
			r.With(alerts).Get("/alerts/statuses", s.handleAlertStatuses)
			r.With(alerts).Get("/alerts/{name}/info", s.handleAlertInfo)
			r.With(alerts).Post("/alerts/{name}/test", s.handleAlertTest)
			r.With(alerts).Get("/alerts/{name}", s.handleAlertGet)
			r.With(alerts).Put("/alerts/{name}", s.handleAlertsSave)
			r.With(alerts).Delete("/alerts/{name}", s.handleAlertDelete)
			r.With(alerts).Post("/alerts/{name}/enable", s.handleAlertEnable(true))
			r.With(alerts).Post("/alerts/{name}/disable", s.handleAlertEnable(false))
			snippets := s.require("snippets", "edit")
			r.With(snippets).Get("/snippets", s.handleSnippetsList)
			r.With(snippets).Post("/snippets", s.handleSnippetsSave)
			r.With(snippets).Put("/snippets", s.handleSnippetsSave)
			r.With(snippets).Get("/snippets/{name}", s.handleSnippetGet)
			r.With(snippets).Put("/snippets/{name}", s.handleSnippetsSave)
			r.With(snippets).Delete("/snippets/{name}", s.handleSnippetDelete)
			r.With(snippets).Get("/snippet-libraries", s.handleLibrariesList)
			r.With(snippets).Post("/snippet-libraries", s.handleLibrariesSave)
			r.With(snippets).Put("/snippet-libraries", s.handleLibrariesSave)
			r.With(snippets).Get("/snippet-libraries/{name}", s.handleLibraryGet)
			r.With(snippets).Put("/snippet-libraries/{name}", s.handleLibrariesSave)
			r.With(snippets).Delete("/snippet-libraries/{name}", s.handleLibraryDelete)
			r.With(s.require("users", "admin")).Get("/users", s.handleUsersList)
			r.With(s.require("users", "admin")).Post("/users", s.handleUserCreate)
			r.With(s.require("users", "admin")).Get("/users/{name}", s.handleUserGet)
			r.With(s.require("users", "admin")).Put("/users/{name}", s.handleUserUpdate)
			r.With(s.require("users", "admin")).Delete("/users/{name}", s.handleUserDelete)
			r.With(s.require("users", "admin")).Post("/users/{name}/password", s.handleUserSetPassword)
			r.With(s.auditAs(AuditPHIAccess), s.require("messages", "content")).Get("/messages/export", s.handleMessagesExport)
			r.With(s.require("messages", "import")).Post("/messages/import", s.handleMessagesImport)
			r.With(s.auditAs(AuditPHIAccess), s.require("messages", "view")).Get("/messages/{id}", s.handleMessageGet)
			r.With(s.auditAs(AuditPHIAccess), s.require("messages", "content")).Get("/messages/{id}/content", s.handleMessageContent)
			r.With(s.require("messages", "view")).Get("/messages/trends", s.handleMessageTrends)
			r.With(s.require("messages", "delete")).Delete("/messages", s.handleMessagesDelete)
			r.With(s.require("messages", "delete")).Delete("/messages/{id}", s.handleMessageDelete)
			r.With(s.require("messages", "send")).Post("/messages/{id}/reprocess", s.handleMessageReprocess)
			// The reply shows the previous attempts' errors, which can quote
			// message content: viewing messages is needed too, as a PHI access.
			r.With(s.auditAs(AuditPHIAccess), s.require("messages", "view"), s.require("messages", "send")).Post("/messages/{id}/requeue", s.handleMessageRequeue)
			r.With(s.require("messages", "send")).Post("/messages/requeue", s.handleMessagesRequeue)
			r.With(s.auditAs(AuditRead), s.require("audit", "view")).Get("/audit", s.handleAuditSearch)
			r.With(s.require("messages", "view")).Get("/system/prune", s.handlePruneStatus)
			r.With(s.require("messages", "delete")).Post("/system/prune/start", s.handlePrune(true))
			r.With(s.require("messages", "delete")).Post("/system/prune/stop", s.handlePrune(false))
		})
	})
	return r
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	if wantsXML(w) && writeXML(w, status, v) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// allowedMethods lists the methods the router serves for path.
func allowedMethods(mux *chi.Mux, path string) string {
	var allowed []string
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if mux.Match(chi.NewRouteContext(), m, path) {
			allowed = append(allowed, m)
		}
	}
	return strings.Join(allowed, ", ")
}
