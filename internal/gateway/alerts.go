package gateway

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"

	"github.com/weavster-dev/weavster/internal/artifact"
)

// Alert definitions (spec §2.7) are artifact.Alert, shared with the
// config-as-code document.
type (
	Alert        = artifact.Alert
	AlertTrigger = artifact.AlertTrigger
	AlertAction  = artifact.AlertAction
)

// AlertStore keeps alert definitions. Save writes every alert or none; with
// create it fails with ErrAlertExists when one already exists, otherwise it
// creates or replaces.
type AlertStore interface {
	ListAlerts(ctx context.Context) ([]Alert, error)
	GetAlert(ctx context.Context, id string) (Alert, error)            // ErrAlertNotFound
	SaveAlerts(ctx context.Context, alerts []Alert, create bool) error // ErrAlertExists
	DeleteAlert(ctx context.Context, id string) error                  // ErrAlertNotFound
	// SetAlertEnabled enables or disables an alert and returns it.
	SetAlertEnabled(ctx context.Context, id string, enabled bool) (Alert, error) // ErrAlertNotFound
}

// Alert store errors.
var (
	ErrAlertNotFound = errors.New("alert not found")
	ErrAlertExists   = errors.New("an alert with that id already exists")
)

// Alert options: the events an alert can trigger on and its action types.
var (
	alertEvents      = artifact.AlertEvents
	alertActionTypes = artifact.AlertActionTypes
)

// checkAlert validates one alert definition; the error is safe to show.
var checkAlert = artifact.CheckAlert

func (s *Server) alertsAvailable(w http.ResponseWriter) bool {
	if s.cfg.Alerts == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "alerts unavailable")
		return false
	}
	return true
}

func writeAlertError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrAlertNotFound):
		writeStatusError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrAlertExists):
		writeStatusError(w, http.StatusConflict, err.Error())
	default:
		writeBackendError(w, err)
	}
}

func (s *Server) handleAlertsList(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAvailable(w) {
		return
	}
	list, err := s.cfg.Alerts.ListAlerts(r.Context())
	if err != nil {
		writeAlertError(w, err)
		return
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	writeJSON(w, http.StatusOK, append([]Alert{}, list...))
}

// handleAlertsSave serves POST (create one) and PUT on one alert.
func (s *Server) handleAlertsSave(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAvailable(w) {
		return
	}
	saveNamed(w, r, "alert", false, checkAlert, s.cfg.Alerts.SaveAlerts, writeAlertError)
}

// handleAlertsImport saves an array of alerts, all or nothing: without
// force=true an existing id is a conflict.
func (s *Server) handleAlertsImport(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAvailable(w) {
		return
	}
	force := false
	if v := r.URL.Query().Get("force"); v != "" {
		var err error
		if force, err = strconv.ParseBool(v); err != nil {
			writeStatusError(w, http.StatusBadRequest, "force must be true or false")
			return
		}
	}
	save := func(ctx context.Context, list []Alert, _ bool) error {
		return s.cfg.Alerts.SaveAlerts(ctx, list, !force)
	}
	saveNamed(w, r, "alert", true, checkAlert, save, writeAlertError)
}

func (s *Server) handleAlertGet(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAvailable(w) {
		return
	}
	id, ok := pathName(w, r)
	if !ok {
		return
	}
	a, err := s.cfg.Alerts.GetAlert(r.Context(), id)
	if err != nil {
		writeAlertError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleAlertDelete(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAvailable(w) {
		return
	}
	id, ok := pathName(w, r)
	if !ok {
		return
	}
	if err := s.cfg.Alerts.DeleteAlert(r.Context(), id); err != nil {
		writeAlertError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAlertEnable serves POST /alerts/{name}/enable and /disable.
func (s *Server) handleAlertEnable(enabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.alertsAvailable(w) {
			return
		}
		id, ok := pathName(w, r)
		if !ok {
			return
		}
		a, err := s.cfg.Alerts.SetAlertEnabled(r.Context(), id, enabled)
		if err != nil {
			writeAlertError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, a)
	}
}

func (s *Server) handleAlertOptions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string][]string{"events": alertEvents, "actionTypes": alertActionTypes})
}
