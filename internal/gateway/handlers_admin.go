package gateway

import "net/http"

func (s *Server) handleSystem(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.cfg.System)
}

func (s *Server) handleTopologyOverview(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Topology == nil {
		http.Error(w, "topology unavailable", http.StatusServiceUnavailable)
		return
	}
	g, err := s.cfg.Topology.Overview(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) handleTopologyFlow(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Topology == nil {
		http.Error(w, "topology unavailable", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("flowId")
	g, err := s.cfg.Topology.FlowInternal(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, g)
}
