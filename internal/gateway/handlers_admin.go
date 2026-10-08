package gateway

import "net/http"

func (s *Server) handleTopologyOverview(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Topology == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "topology unavailable")
		return
	}
	g, err := s.cfg.Topology.Overview(r.Context())
	if err != nil {
		writeFlowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) handleTopologyFlow(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Topology == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "topology unavailable")
		return
	}
	id := r.PathValue("flowId")
	g, err := s.cfg.Topology.FlowInternal(r.Context(), id)
	if err != nil {
		writeFlowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}
