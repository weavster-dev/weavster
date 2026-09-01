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
		r.Use(s.Authenticate)
		if s.cfg.RequireCSRF {
			r.Use(RequireMarkerHeader)
		}

		// System — any authenticated principal may read.
		r.Group(func(r chi.Router) {
			r.Use(s.Authorize("system", "view"))
			r.Get("/system", s.handleSystem)
		})

		// Topology — requires topology:view.
		r.Group(func(r chi.Router) {
			r.Use(s.Authorize("topology", "view"))
			r.Get("/topology", s.handleTopologyOverview)
			r.Get("/topology/flows/{flowId}", s.handleTopologyFlow)
		})

		// Flows read — requires flows:view.
		r.Group(func(r chi.Router) {
			r.Use(s.Authorize("flows", "view"))
			r.Get("/flows", s.handleFlowsList)
			r.Get("/flows/{id}", s.handleFlowsGet)
		})

		// Flows mutate — requires flows:edit.
		r.Group(func(r chi.Router) {
			r.Use(s.Authorize("flows", "edit"))
			r.Post("/flows", s.handleFlowsCreate)
			r.Delete("/flows/{id}", s.handleFlowsDelete)
		})

		// Messages — requires messages:view.
		r.Group(func(r chi.Router) {
			r.Use(s.Authorize("messages", "view"))
			r.Get("/messages", s.handleMessagesSearch)
		})
	})
	return r
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
