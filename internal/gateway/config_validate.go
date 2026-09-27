package gateway

import (
	"errors"
	"io"
	"net/http"
)

// ConfigSummary counts what a valid config-as-code document holds.
type ConfigSummary struct {
	Flows            int `json:"flows"`
	Alerts           int `json:"alerts"`
	Snippets         int `json:"snippets"`
	SnippetLibraries int `json:"snippetLibraries"`
	Scripts          int `json:"scripts"`
	ConfigMap        int `json:"configmap"`
	Settings         int `json:"settings"`
}

// ConfigValidator checks a config-as-code document (YAML or JSON); the
// error explains what is wrong and is safe to show.
type ConfigValidator interface {
	ValidateConfig(doc []byte) (ConfigSummary, error)
}

// handleConfigValidate checks a config-as-code document without changing
// anything: 200 with its counts, or 400 naming every problem.
func (s *Server) handleConfigValidate(w http.ResponseWriter, r *http.Request) {
	if s.cfg.ConfigValidator == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "configuration validation unavailable")
		return
	}
	doc, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxImportBytes))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeStatusError(w, http.StatusRequestEntityTooLarge, "configuration document larger than 50 MiB")
		return
	}
	if err != nil {
		writeStatusError(w, http.StatusBadRequest, "could not read the body")
		return
	}
	summary, err := s.cfg.ConfigValidator.ValidateConfig(doc)
	if err != nil {
		writeStatusError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "counts": summary})
}
