package gateway

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
)

// validFlowID restricts flow IDs to one URL path segment.
var validFlowID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// writeFlowError maps FlowStore errors to 404/409, and anything else to a
// 500 that does not leak internal detail.
func writeFlowError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrFlowNotFound):
		http.Error(w, "flow not found", http.StatusNotFound)
	case errors.Is(err, ErrFlowExists):
		http.Error(w, "flow already exists", http.StatusConflict)
	case errors.Is(err, ErrFlowNotRunning):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrInvalidFlow), errors.Is(err, ErrInvalidMessage):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func (s *Server) handleFlowsList(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Flows == nil {
		http.Error(w, "flows unavailable", http.StatusServiceUnavailable)
		return
	}
	flows, err := s.cfg.Flows.List(r.Context())
	if err != nil {
		writeFlowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, flows)
}

func (s *Server) handleFlowsGet(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Flows == nil {
		http.Error(w, "flows unavailable", http.StatusServiceUnavailable)
		return
	}
	f, err := s.cfg.Flows.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeFlowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (s *Server) handleFlowsCreate(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Flows == nil {
		http.Error(w, "flows unavailable", http.StatusServiceUnavailable)
		return
	}
	var f Flow
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if f.ID == "" {
		http.Error(w, "flow id is required", http.StatusBadRequest)
		return
	}
	if !validFlowID.MatchString(f.ID) {
		http.Error(w, "flow id must be 1-128 characters from A-Z a-z 0-9 . _ -", http.StatusBadRequest)
		return
	}
	if err := s.cfg.Flows.Create(r.Context(), f); err != nil {
		writeFlowError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

func (s *Server) handleFlowsDelete(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Flows == nil {
		http.Error(w, "flows unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.cfg.Flows.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeFlowError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// maxMessageBytes caps a received message body.
const maxMessageBytes = 10 << 20

func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Ingest == nil {
		http.Error(w, "message processing unavailable", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxMessageBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "message body larger than 10 MiB", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "could not read message body", http.StatusBadRequest)
		return
	}
	res, err := s.cfg.Ingest.Ingest(r.Context(), r.PathValue("id"), body)
	if err != nil {
		writeFlowError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, res)
}
