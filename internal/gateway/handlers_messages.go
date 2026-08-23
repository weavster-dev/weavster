package gateway

import "net/http"

func (s *Server) handleMessagesSearch(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Messages == nil {
		http.Error(w, "messages unavailable", http.StatusServiceUnavailable)
		return
	}
	q := MessageQuery{
		Status: r.URL.Query().Get("status"),
		FlowID: r.URL.Query().Get("flowId"),
	}
	if q.Limit == 0 {
		q.Limit = 100
	}
	msgs, err := s.cfg.Messages.Search(r.Context(), q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}
