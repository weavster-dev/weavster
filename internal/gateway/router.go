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
		if s.cfg.RequireCSRF {
			r.Use(RequireMarkerHeader)
		}
		r.Get("/system", s.handleSystem)
		r.Get("/topology", s.handleTopologyOverview)
		r.Get("/topology/flows/{flowId}", s.handleTopologyFlow)
		r.Get("/flows", s.handleFlowsList)
		r.Post("/flows", s.handleFlowsCreate)
		r.Get("/flows/{id}", s.handleFlowsGet)
		r.Delete("/flows/{id}", s.handleFlowsDelete)
		r.Get("/messages", s.handleMessagesSearch)
	})
	return r
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
