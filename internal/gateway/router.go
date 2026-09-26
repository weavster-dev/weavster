package gateway

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Router builds the chi router with middleware and routes.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(SecurityHeaders)
	r.Use(BlockTrace)

	// Unauthenticated metadata.
	r.Get("/api/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte(OpenAPISpec()))
	})

	r.Route("/api/v1", func(r chi.Router) {
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
			r.With(s.require("flows", "edit")).Post("/flows", s.handleFlowsCreate)
			r.With(s.require("flows", "view")).Get("/flows/{id}", s.handleFlowsGet)
			r.With(s.require("flows", "edit")).Delete("/flows/{id}", s.handleFlowsDelete)
			r.With(s.require("messages", "send")).Post("/flows/{id}/messages", s.handleIngest)
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
