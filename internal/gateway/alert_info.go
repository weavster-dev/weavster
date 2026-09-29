package gateway

import (
	"errors"
	"io"
	"net/http"
	"slices"

	"github.com/weavster-dev/weavster/internal/artifact"
)

// alertStatus is one entry of GET /api/v1/alerts/statuses.
type alertStatus struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

func (s *Server) handleAlertStatuses(w http.ResponseWriter, r *http.Request) {
	list, ok := s.sortedAlerts(w, r)
	if !ok {
		return
	}
	out := make([]alertStatus, 0, len(list))
	for _, a := range list {
		out = append(out, alertStatus{ID: a.ID, Name: a.Name, Enabled: a.Enabled})
	}
	writeJSON(w, http.StatusOK, out)
}

// alertInfo is the reply of GET /api/v1/alerts/{id}/info: the alert and
// the values its trigger and actions may use.
type alertInfo struct {
	Alert       Alert    `json:"alert"`
	Events      []string `json:"events"`
	ActionTypes []string `json:"actionTypes"`
}

func (s *Server) handleAlertInfo(w http.ResponseWriter, r *http.Request) {
	a, ok := s.alertFor(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, alertInfo{Alert: a, Events: artifact.AlertEvents, ActionTypes: artifact.AlertActionTypes})
}

// alertTestResult is the reply of POST /api/v1/alerts/{id}/test.
type alertTestResult struct {
	Matches   bool          `json:"matches"`
	Enabled   bool          `json:"enabled"`
	Event     string        `json:"event"`
	FlowID    string        `json:"flowId,omitempty"`
	Actions   []AlertAction `json:"actions"`
	Delivered bool          `json:"delivered"`
}

// handleAlertTest is a dry run: it says whether an event of a flow would
// trigger the alert and which actions it would run. Nothing is sent
// (delivery is not part of this release, #107 D-96).
func (s *Server) handleAlertTest(w http.ResponseWriter, r *http.Request) {
	a, ok := s.alertFor(w, r)
	if !ok {
		return
	}
	var req struct {
		Event  string `json:"event"`
		FlowID string `json:"flowId"`
	}
	if !decodeOptional(w, r, &req) {
		return
	}
	if req.Event == "" && len(a.Trigger.Events) > 0 {
		req.Event = a.Trigger.Events[0]
	}
	if req.FlowID == "" && len(a.Trigger.Flows) > 0 {
		req.FlowID = a.Trigger.Flows[0]
	}
	if !slices.Contains(artifact.AlertEvents, req.Event) {
		writeStatusError(w, http.StatusBadRequest, "event must be one of the trigger events in GET /api/v1/alerts/options")
		return
	}
	if req.FlowID != "" {
		if err := artifact.CheckValue("flowId", req.FlowID); err != nil {
			writeStatusError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	matches := slices.Contains(a.Trigger.Events, req.Event) &&
		(len(a.Trigger.Flows) == 0 || slices.Contains(a.Trigger.Flows, req.FlowID))
	actions := a.Actions
	if !matches {
		actions = []AlertAction{}
	}
	writeJSON(w, http.StatusOK, alertTestResult{Matches: matches, Enabled: a.Enabled, Event: req.Event, FlowID: req.FlowID, Actions: actions})
}

// alertFor loads the alert named in the path.
func (s *Server) alertFor(w http.ResponseWriter, r *http.Request) (Alert, bool) {
	if !s.alertsAvailable(w) {
		return Alert{}, false
	}
	id, ok := pathName(w, r)
	if !ok {
		return Alert{}, false
	}
	a, err := s.cfg.Alerts.GetAlert(r.Context(), id)
	if err != nil {
		writeAlertError(w, err)
		return Alert{}, false
	}
	return a, true
}

// decodeOptional is decodeStrict for a body that may be left out.
func decodeOptional(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := decodeJSON(w, r, v); err != nil && !errors.Is(err, io.EOF) {
		writeStatusError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}
