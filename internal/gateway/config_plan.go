package gateway

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// ConfigPlan is what applying a config-as-code document would change on the
// server (spec D-04, #107 D-47).
type ConfigPlan struct {
	// Fingerprint identifies the live configuration the plan was made
	// against.
	Fingerprint string         `json:"fingerprint"`
	Added       []string       `json:"added"`
	Updated     []string       `json:"updated"`
	Removed     []string       `json:"removed"`
	Unchanged   int            `json:"unchanged"`
	Changes     []ConfigChange `json:"changes"`
	// Text is the plan for people: +, ~ (with changed fields), and -.
	Text string `json:"text"`
}

// ConfigChange is one planned change: add, update, or remove.
type ConfigChange struct {
	Key    string              `json:"key"`
	Action string              `json:"action"`
	Before json.RawMessage     `json:"before,omitempty"`
	After  json.RawMessage     `json:"after,omitempty"`
	Fields []ConfigFieldChange `json:"fields,omitempty"`
}

// ConfigFieldChange is one changed value in an updated artifact.
type ConfigFieldChange struct {
	Path   string          `json:"path"`
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
}

// ConfigPlanner plans a config-as-code document against the live
// configuration (as export gathers it). ErrInvalidConfig marks a document
// problem (its message is safe to show).
type ConfigPlanner interface {
	PlanConfig(doc []byte, live ConfigBundle) (ConfigPlan, error)
}

// ErrInvalidConfig: the config-as-code document is not valid.
var ErrInvalidConfig = errors.New("invalid configuration document")

// handleConfigPlan answers what applying the document would change.
func (s *Server) handleConfigPlan(w http.ResponseWriter, r *http.Request) {
	if s.cfg.ConfigPlanner == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "configuration planning unavailable")
		return
	}
	if !s.configPorts(w, false, false) {
		return
	}
	doc, ok := s.configDocument(w, r)
	if !ok {
		return
	}
	live, err := s.liveConfig(r.Context(), true)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	plan, err := s.cfg.ConfigPlanner.PlanConfig(doc, live)
	if errors.Is(err, ErrInvalidConfig) {
		writeStatusError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

// readConfigBody reads a config-as-code document of at most maxImportBytes,
// answering 413 or 400 and returning false on failure.
func readConfigBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	doc, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxImportBytes))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeStatusError(w, http.StatusRequestEntityTooLarge, "configuration document larger than 50 MiB")
		return nil, false
	}
	if err != nil {
		writeStatusError(w, http.StatusBadRequest, "could not read the body")
		return nil, false
	}
	return doc, true
}
