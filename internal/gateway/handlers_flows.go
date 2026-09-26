package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// writeFlowError maps FlowStore errors to 404/409, and anything else to a
// 500 that does not leak internal detail.
func writeFlowError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrFlowNotFound):
		http.Error(w, "flow not found", http.StatusNotFound)
	case errors.Is(err, ErrUnknownAction), errors.Is(err, ErrDestinationNotFound):
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

// runtimeFieldError explains why a definition must not carry runtime state.
func runtimeFieldError(doc map[string]any) string {
	if _, present := doc["status"]; present {
		return "status is managed by lifecycle operations (deploy, start, ...); omit it"
	}
	if _, present := doc["stoppedDestinations"]; present {
		return "stoppedDestinations is managed by POST /api/v1/flows/{id}/destinations/{name}/{start,stop}; omit it"
	}
	return ""
}

// decodeFlow reads a flow definition from the request body. Clients never
// send status (lifecycle operations own it); pathID, when set, must match
// any id in the body.
func decodeFlow(w http.ResponseWriter, r *http.Request, pathID string) (Flow, map[string]any, bool) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "could not read request body", http.StatusBadRequest)
		return Flow{}, nil, false
	}
	doc, err := parseFlowDoc(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return Flow{}, nil, false
	}
	if msg := runtimeFieldError(doc); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return Flow{}, nil, false
	}
	if pathID != "" {
		switch id := doc["id"].(type) {
		case nil: // absent or null: the id comes from the URL
			doc["id"] = pathID
		case string:
			if id != pathID {
				http.Error(w, "flow id cannot be changed; the id in the body must match the URL", http.StatusBadRequest)
				return Flow{}, nil, false
			}
		}
	}
	// The schema enforces the id format and reserved ids too.
	if err := validateFlowDoc(doc); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return Flow{}, nil, false
	}
	normalized, _ := json.Marshal(doc) // a valid document re-encodes
	var f Flow
	if err := json.Unmarshal(normalized, &f); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return Flow{}, nil, false
	}
	return f, doc, true
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
	f, doc, ok := decodeFlow(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	// enable/disable own this flag; an update that omits it keeps it.
	_, setsEnabled := doc["enabled"]
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

// decodeFlowList validates each flow definition of a list body (import,
// bulk update). It returns the flows and, for each, whether it sets enabled.
func decodeFlowList(w http.ResponseWriter, raws []json.RawMessage) ([]Flow, []bool, bool) {
	flows := make([]Flow, 0, len(raws))
	setsEnabled := make([]bool, 0, len(raws))
	for i, raw := range raws {
		doc, err := parseFlowDoc(raw)
		if err != nil {
			http.Error(w, fmt.Sprintf("flows[%d]: not a flow object", i), http.StatusBadRequest)
			return nil, nil, false
		}
		if msg := runtimeFieldError(doc); msg != "" {
			http.Error(w, fmt.Sprintf("flows[%d]: %s", i, msg), http.StatusBadRequest)
			return nil, nil, false
		}
		// The schema also enforces the id format and reserved ids.
		if err := validateFlowDoc(doc); err != nil {
			http.Error(w, fmt.Sprintf("flows[%d]: %v", i, err), http.StatusBadRequest)
			return nil, nil, false
		}
		var f Flow
		if err := json.Unmarshal(raw, &f); err != nil {
			http.Error(w, fmt.Sprintf("flows[%d]: not a flow object", i), http.StatusBadRequest)
			return nil, nil, false
		}
		_, sets := doc["enabled"]
		flows = append(flows, f)
		setsEnabled = append(setsEnabled, sets)
	}
	return flows, setsEnabled, true
}

// flowsDocument is the body of an import or a bulk update. Other
// top-level fields are rejected.
type flowsDocument struct {
	// Version is raw so a bulk update can reject it even when null.
	Version json.RawMessage    `json:"version"`
	Flows   *[]json.RawMessage `json:"flows"`
}

// decodeFlowsBody reads a flowsDocument of at most maxImportBytes. On
// failure it writes the error response, naming the expected shape.
func decodeFlowsBody(w http.ResponseWriter, r *http.Request, shape string) (flowsDocument, bool) {
	var doc flowsDocument
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxImportBytes))
	dec.DisallowUnknownFields()
	err := dec.Decode(&doc)
	if err == nil && doc.Flows == nil {
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
			http.Error(w, "document larger than 50 MiB", http.StatusRequestEntityTooLarge)
			return doc, false
		}
		http.Error(w, "body must be "+shape, http.StatusBadRequest)
		return doc, false
	}
	return doc, true
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
	bundle, ok := decodeFlowsBody(w, r, `an export document: {"version":1,"flows":[...]}`)
	if !ok {
		return
	}
	var version int
	if json.Unmarshal(bundle.Version, &version) != nil || version != FlowBundleVersion {
		http.Error(w, fmt.Sprintf("unsupported export version %s; expected %d", bundle.Version, FlowBundleVersion), http.StatusBadRequest)
		return
	}
	flows, _, ok := decodeFlowList(w, *bundle.Flows)
	if !ok {
		return
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

func (s *Server) handleDestinationAction(w http.ResponseWriter, r *http.Request) {
	var running bool
	switch r.PathValue("action") {
	case "start":
		running = true
	case "stop":
	default:
		http.NotFound(w, r)
		return
	}
	if s.cfg.Lifecycle == nil {
		http.Error(w, "flow lifecycle unavailable", http.StatusServiceUnavailable)
		return
	}
	f, err := s.cfg.Lifecycle.SetDestinationRunning(r.Context(), r.PathValue("id"), r.PathValue("dest"), running)
	if err != nil {
		writeFlowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (s *Server) handleFlowsBulkUpdate(w http.ResponseWriter, r *http.Request) {
	if s.cfg.FlowUpdates == nil {
		http.Error(w, "flow updates unavailable", http.StatusServiceUnavailable)
		return
	}
	body, ok := decodeFlowsBody(w, r, `{"flows":[...]}`)
	if !ok {
		return
	}
	if body.Version != nil {
		http.Error(w, `body must be {"flows":[...]}; version belongs to import documents`, http.StatusBadRequest)
		return
	}
	flows, setsEnabled, ok := decodeFlowList(w, *body.Flows)
	if !ok {
		return
	}
	changes := make([]FlowChange, len(flows))
	for i, f := range flows {
		// As with PUT /flows/{id}: omitting enabled keeps it.
		changes[i] = FlowChange{Flow: f, KeepEnabled: !setsEnabled[i]}
	}
	updated, err := s.cfg.FlowUpdates.UpdateMany(r.Context(), changes)
	switch {
	case errors.Is(err, ErrUpdateIncomplete):
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   map[string]string{"code": "UPDATE_INCOMPLETE", "message": "bulk update stopped part-way; updated lists what was written"},
			"updated": updated,
		})
	case errors.Is(err, ErrFlowNotFound):
		http.Error(w, err.Error(), http.StatusNotFound) // names the missing flows
	case err != nil:
		writeFlowError(w, err)
	default:
		writeJSON(w, http.StatusOK, map[string][]string{"updated": updated})
	}
}

// ConnectorNames lists one flow's connectors.
type ConnectorNames struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	SourceType   string   `json:"sourceType"`
	Destinations []string `json:"destinations"`
}

func (s *Server) handleConnectorNames(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Flows == nil {
		http.Error(w, "flows unavailable", http.StatusServiceUnavailable)
		return
	}
	flows, err := s.cfg.Flows.List(r.Context())
	if err != nil {
		writeFlowError(w, err)
		return
	}
	out := make([]ConnectorNames, 0, len(flows))
	for _, f := range flows {
		c := ConnectorNames{ID: f.ID, Name: f.Name, SourceType: f.SourceType, Destinations: make([]string, 0, len(f.Destinations))}
		for _, d := range f.Destinations {
			c.Destinations = append(c.Destinations, d.Name)
		}
		out = append(out, c)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePortsInUse(w http.ResponseWriter, _ *http.Request) {
	ports := s.cfg.Listeners
	if ports == nil {
		ports = []PortInUse{}
	}
	writeJSON(w, http.StatusOK, ports)
}
