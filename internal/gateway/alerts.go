package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"sort"
	"strconv"
)

// Alert is an alert definition (spec §2.7): which processing events of which
// flows trigger it, and whom it notifies.
type Alert struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Enabled bool          `json:"enabled"`
	Trigger AlertTrigger  `json:"trigger"`
	Actions []AlertAction `json:"actions"`
}

// AlertTrigger selects the events an alert reacts to; no flows means every
// flow.
type AlertTrigger struct {
	Events []string `json:"events"`
	Flows  []string `json:"flows,omitempty"`
}

// AlertAction is one notification: email (to) or webhook (url).
type AlertAction struct {
	Type string   `json:"type"`
	To   []string `json:"to,omitempty"`
	URL  string   `json:"url,omitempty"`
}

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
	alertEvents      = []string{"message.errored", "message.queued", "message.dead-lettered"}
	alertActionTypes = []string{"email", "webhook"}
)

func (a *Alert) nameRef() *string { return &a.ID }

// checkAlert validates one alert definition; the error is safe to show.
func checkAlert(a Alert) error {
	if a.ID == "import" || a.ID == "options" {
		return fmt.Errorf("id %q is reserved", a.ID)
	}
	if a.Name == "" || len(a.Name) > 200 {
		return fmt.Errorf("alert %s: name must be 1-200 characters", a.ID)
	}
	if len(a.Trigger.Events) == 0 {
		return fmt.Errorf("alert %s: trigger.events needs at least one of %v", a.ID, alertEvents)
	}
	for _, e := range a.Trigger.Events {
		if !contains(alertEvents, e) {
			return fmt.Errorf("alert %s: unknown trigger event %q; use %v", a.ID, e, alertEvents)
		}
	}
	for _, f := range a.Trigger.Flows {
		if err := checkName(f); err != nil {
			return fmt.Errorf("alert %s: trigger.flows: %w", a.ID, err)
		}
	}
	if len(a.Actions) == 0 {
		return fmt.Errorf("alert %s: actions needs at least one action", a.ID)
	}
	for i, act := range a.Actions {
		if err := checkAlertAction(act); err != nil {
			return fmt.Errorf("alert %s: actions[%d]: %w", a.ID, i, err)
		}
	}
	return nil
}

func checkAlertAction(act AlertAction) error {
	switch act.Type {
	case "email":
		if len(act.To) == 0 || act.URL != "" {
			return errors.New("an email action needs to (a list of addresses) and no url")
		}
		for _, addr := range act.To {
			if _, err := mail.ParseAddress(addr); err != nil {
				return fmt.Errorf("%q is not an email address", addr)
			}
		}
	case "webhook":
		u, err := url.Parse(act.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(act.To) != 0 {
			return errors.New("a webhook action needs url (http or https) and no to")
		}
	default:
		return fmt.Errorf("type must be one of %v", alertActionTypes)
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

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
