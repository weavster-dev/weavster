package gateway

import (
	"context"
	"encoding/json"
	"net/http"
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
	r := chi.NewRouter()
	r.Use(apiVersion)
	r.Use(SecurityHeaders)
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
			r.Get("/system", s.handleSystem)
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
			r.With(s.require("flows", "deploy")).Post("/flows/{id}/{action}", s.handleFlowAction)
			r.With(s.require("flows", "deploy")).Post("/flows/{id}/destinations/{dest}/{action}", s.handleDestinationAction)
			r.With(s.require("flows", "view")).Get("/flows/{id}/stats", s.handleFlowStats)
			r.With(s.require("events", "view")).Get("/events", s.handleEvents)
			r.With(s.auditAs(AuditPHIAccess), s.require("messages", "view")).Get("/messages", s.handleMessagesSearch)
		})
	})
	return r
}

func writeJSON(w http.ResponseWriter, status int, v any) {
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
