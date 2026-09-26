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
	case errors.Is(err, ErrUnknownAction):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrFlowExists):
		http.Error(w, "flow already exists", http.StatusConflict)
	case errors.Is(err, ErrFlowNotRunning), errors.Is(err, ErrInvalidTransition):
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

// decodeFlow reads a flow definition from the request body. Clients never
// send status (lifecycle operations own it); pathID, when set, must match
// any id in the body.
func decodeFlow(w http.ResponseWriter, r *http.Request, pathID string) (Flow, map[string]json.RawMessage, bool) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "could not read request body", http.StatusBadRequest)
		return Flow{}, nil, false
	}
	var f Flow
	if err := json.Unmarshal(body, &f); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return Flow{}, nil, false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		http.Error(w, "the body must be a JSON object", http.StatusBadRequest)
		return Flow{}, nil, false
	}
	if _, present := fields["status"]; present {
		http.Error(w, "status is managed by lifecycle operations (deploy, start, ...); omit it", http.StatusBadRequest)
		return Flow{}, nil, false
	}
	if pathID != "" {
		if f.ID != "" && f.ID != pathID {
			http.Error(w, "flow id cannot be changed; the id in the body must match the URL", http.StatusBadRequest)
			return Flow{}, nil, false
		}
		f.ID = pathID
	}
	if f.ID == "" {
		http.Error(w, "flow id is required", http.StatusBadRequest)
		return Flow{}, nil, false
	}
	if !validFlowID.MatchString(f.ID) {
		http.Error(w, "flow id must be 1-128 characters from A-Z a-z 0-9 . _ -", http.StatusBadRequest)
		return Flow{}, nil, false
	}
	return f, fields, true
}

func (s *Server) handleFlowsCreate(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Flows == nil {
		http.Error(w, "flows unavailable", http.StatusServiceUnavailable)
		return
	}
	f, _, ok := decodeFlow(w, r, "")
	if !ok {
		return
	}
	stored, err := s.cfg.Flows.Create(r.Context(), f)
	if err != nil {
		writeFlowError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, stored)
}

func (s *Server) handleFlowsUpdate(w http.ResponseWriter, r *http.Request) {
	if s.cfg.FlowUpdates == nil {
		http.Error(w, "flow updates unavailable", http.StatusServiceUnavailable)
		return
	}
	f, fields, ok := decodeFlow(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	// enable/disable own this flag; an update that omits it keeps it.
	_, setsEnabled := fields["enabled"]
	updated, err := s.cfg.FlowUpdates.Update(r.Context(), f.ID, f, !setsEnabled)
	if err != nil {
		writeFlowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleFlowEnable(enabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.FlowUpdates == nil {
			http.Error(w, "flow updates unavailable", http.StatusServiceUnavailable)
			return
		}
		f, err := s.cfg.FlowUpdates.SetEnabled(r.Context(), r.PathValue("id"), enabled)
		if err != nil {
			writeFlowError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, f)
	}
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

func (s *Server) handleFlowAction(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	if s.cfg.Lifecycle == nil {
		http.Error(w, "flow lifecycle unavailable", http.StatusServiceUnavailable)
		return
	}
	f, err := s.cfg.Lifecycle.Transition(r.Context(), r.PathValue("id"), action)
	if err != nil {
		writeFlowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (s *Server) handleRedeployAll(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Lifecycle == nil {
		http.Error(w, "flow lifecycle unavailable", http.StatusServiceUnavailable)
		return
	}
	flows, err := s.cfg.Lifecycle.RedeployAll(r.Context())
	if err != nil {
		// Report the flows already redeployed so the caller knows the state.
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":      map[string]string{"code": "REDEPLOY_INCOMPLETE", "message": "redeploy-all stopped before finishing; see redeployed"},
			"redeployed": flows,
		})
		return
	}
	writeJSON(w, http.StatusOK, flows)
}
