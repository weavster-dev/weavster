package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// flowIDPattern restricts flow IDs to one URL path segment.
var flowIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// reservedFlowIDs are path segments used by /flows/<name> routes.
var reservedFlowIDs = map[string]bool{"export": true, "import": true, "redeploy-all": true}

type flowIDValidator struct{}

// MatchString reports whether id is a usable flow id.
func (flowIDValidator) MatchString(id string) bool {
	return flowIDPattern.MatchString(id) && !reservedFlowIDs[id]
}

// validFlowID accepts 1-128 characters from A-Z a-z 0-9 . _ - except the
// reserved route names.
var validFlowID = flowIDValidator{}

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
	case errors.Is(err, ErrFlowNotRunning), errors.Is(err, ErrInvalidTransition), errors.Is(err, ErrFlowInUse), errors.Is(err, ErrImportConflict), errors.Is(err, ErrDependency):
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
		http.Error(w, "flow id must be 1-128 characters from A-Z a-z 0-9 . _ - and not export, import, or redeploy-all", http.StatusBadRequest)
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

// FlowBundleVersion is the export/import document version.
const FlowBundleVersion = 1

// maxImportBytes caps an import document.
const maxImportBytes = 50 << 20

func (s *Server) handleFlowsExport(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Transfer == nil {
		http.Error(w, "flow export unavailable", http.StatusServiceUnavailable)
		return
	}
	var ids []string
	if v := r.URL.Query().Get("ids"); v != "" {
		ids = strings.Split(v, ",")
	}
	flows, err := s.cfg.Transfer.Export(r.Context(), ids)
	if err != nil {
		writeFlowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, FlowBundle{Version: FlowBundleVersion, Flows: flows})
}

func (s *Server) handleFlowsImport(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Transfer == nil {
		http.Error(w, "flow import unavailable", http.StatusServiceUnavailable)
		return
	}
	overwrite := false
	if v := r.URL.Query().Get("overwrite"); v != "" {
		var err error
		if overwrite, err = strconv.ParseBool(v); err != nil {
			http.Error(w, "overwrite must be true or false", http.StatusBadRequest)
			return
		}
	}
	var bundle struct {
		Version int                `json:"version"`
		Flows   *[]json.RawMessage `json:"flows"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxImportBytes))
	err := dec.Decode(&bundle)
	if err == nil && bundle.Flows == nil {
		err = errors.New("missing flows array")
	}
	if err == nil {
		if _, tokErr := dec.Token(); tokErr != io.EOF {
			err = errors.New("trailing data after the document")
		}
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "export document larger than 50 MiB", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "body must be an export document: {\"version\":1,\"flows\":[...]}", http.StatusBadRequest)
		return
	}
	if bundle.Version != FlowBundleVersion {
		http.Error(w, fmt.Sprintf("unsupported export version %d; expected %d", bundle.Version, FlowBundleVersion), http.StatusBadRequest)
		return
	}
	flows := make([]Flow, 0, len(*bundle.Flows))
	for i, raw := range *bundle.Flows {
		var f Flow
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &f); err != nil || json.Unmarshal(raw, &fields) != nil || fields == nil {
			http.Error(w, fmt.Sprintf("flows[%d]: not a flow object", i), http.StatusBadRequest)
			return
		}
		if _, present := fields["status"]; present {
			http.Error(w, fmt.Sprintf("flows[%d]: status is managed by lifecycle operations; omit it", i), http.StatusBadRequest)
			return
		}
		if !validFlowID.MatchString(f.ID) {
			http.Error(w, fmt.Sprintf("flows[%d]: flow id must be 1-128 characters from A-Z a-z 0-9 . _ - and not export, import, or redeploy-all", i), http.StatusBadRequest)
			return
		}
		flows = append(flows, f)
	}
	res, err := s.cfg.Transfer.Import(r.Context(), flows, overwrite)
	if errors.Is(err, ErrImportIncomplete) {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   map[string]string{"code": "IMPORT_INCOMPLETE", "message": "import stopped part-way; created and updated list what was written"},
			"created": res.Created, "updated": res.Updated,
		})
		return
	}
	if err != nil {
		writeFlowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
