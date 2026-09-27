package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// configStep is one write of an apply, with the write that undoes it.
type configStep struct {
	key        string
	do, undo   func(context.Context) error
	removeFlow bool // flow removals run dependents-first
}

// handleConfigApply applies a config-as-code document (spec D-04, #107
// D-48): it plans again, refuses a stale fingerprint, applies the changes,
// and rolls every applied change back when one fails. Applies are
// serialized; the audit record carries the plan, reason, and result.
func (s *Server) handleConfigApply(w http.ResponseWriter, r *http.Request) {
	if s.cfg.ConfigPlanner == nil || s.cfg.Flows == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "configuration apply unavailable")
		return
	}
	if !s.configPorts(w, false, false) {
		return
	}
	opts, ok := boolParams(w, r, "dryRun")
	if !ok {
		return
	}
	q := r.URL.Query()
	fingerprint, reason := q.Get("fingerprint"), q.Get("reason")
	if fingerprint == "" {
		writeStatusError(w, http.StatusBadRequest, "fingerprint is required: plan first (POST /api/v1/config/plan) and pass the plan's fingerprint")
		return
	}
	if utf8.RuneCountInString(reason) > 500 {
		writeStatusError(w, http.StatusBadRequest, "reason must be at most 500 characters")
		return
	}
	doc, ok := readConfigBody(w, r)
	if !ok {
		return
	}
	s.applyMu.Lock()
	defer s.applyMu.Unlock()
	ctx := r.Context()
	live, err := s.liveConfig(ctx, true)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	plan, err := s.cfg.ConfigPlanner.PlanConfig(doc, live)
	if errors.Is(err, ErrInvalidConfig) {
		s.auditApply(r, plan, "invalid")
		writeStatusError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeBackendError(w, err)
		return
	}
	if plan.Fingerprint != fingerprint {
		s.auditApply(r, plan, "stale")
		writeStatusError(w, http.StatusConflict, "the configuration changed since the plan was made; plan again and review the new plan")
		return
	}
	if opts["dryRun"] {
		s.auditApply(r, plan, "dry run")
		writeJSON(w, http.StatusOK, map[string]any{"applied": false, "plan": plan})
		return
	}
	steps, err := s.applySteps(plan)
	if err != nil {
		s.auditApply(r, plan, "invalid")
		writeStatusError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Writes finish even if the client goes away, so nothing is left half done.
	ctx = context.WithoutCancel(ctx)
	if failed, err := runSteps(ctx, steps); err != nil {
		result, status, msg := "rolled back", http.StatusConflict, fmt.Sprintf("apply stopped at %s: %v; every change was rolled back", failed, err)
		if !isConflict(err) {
			status = http.StatusInternalServerError
			msg = fmt.Sprintf("apply stopped at %s because of an internal error; every change was rolled back", failed)
		}
		var rb *rollbackError
		if errors.As(err, &rb) {
			result, status = "rollback failed", http.StatusInternalServerError
			msg = fmt.Sprintf("apply stopped at %s, and undoing %s failed: the configuration is partly applied; plan again to see where it stands", failed, rb.key)
		}
		s.auditApply(r, plan, result)
		writeStatusError(w, status, msg)
		return
	}
	s.auditApply(r, plan, "applied")
	writeJSON(w, http.StatusOK, map[string]any{"applied": true, "plan": plan})
}

// isConflict reports errors that come from the configuration itself (a
// flow still in use, an id taken meanwhile) rather than the server.
func isConflict(err error) bool {
	for _, e := range []error{ErrFlowInUse, ErrImportConflict, ErrInvalidFlow, ErrAlertExists, ErrSnippetExists, ErrLibraryExists,
		ErrLibraryInUse, ErrLibraryNotFound, ErrFlowNotFound, ErrAlertNotFound, ErrSnippetNotFound, ErrItemNotFound} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// rollbackError: undoing an applied step failed.
type rollbackError struct {
	key string
	err error
}

func (e *rollbackError) Error() string { return "undo " + e.key + ": " + e.err.Error() }
func (e *rollbackError) Unwrap() error { return e.err }

// runSteps runs the steps in order; when one fails it undoes the applied
// ones in reverse and returns the failed key. Flow removals are retried
// until no more succeed, so dependents go before the flows they use.
func runSteps(ctx context.Context, steps []configStep) (string, error) {
	var done []configStep
	undo := func(failed string, cause error) (string, error) {
		for i := len(done) - 1; i >= 0; i-- {
			if err := done[i].undo(ctx); err != nil {
				return failed, &rollbackError{key: done[i].key, err: err}
			}
		}
		return failed, cause
	}
	var flowRemovals []configStep
	for _, st := range steps {
		if st.removeFlow {
			flowRemovals = append(flowRemovals, st)
			continue
		}
		if err := st.do(ctx); err != nil {
			return undo(st.key, err)
		}
		done = append(done, st)
	}
	for len(flowRemovals) > 0 {
		var left []configStep
		var lastErr error
		for _, st := range flowRemovals {
			if err := st.do(ctx); errors.Is(err, ErrFlowInUse) {
				left, lastErr = append(left, st), err
			} else if err != nil {
				return undo(st.key, err)
			} else {
				done = append(done, st)
			}
		}
		if len(left) == len(flowRemovals) {
			return undo(left[0].key, lastErr) // still in use by a flow the document keeps
		}
		flowRemovals = left
	}
	return "", nil
}

// applySteps turns the plan's changes into ordered writes with their undo.
func (s *Server) applySteps(plan ConfigPlan) ([]configStep, error) {
	order := map[string]int{"library/": 0, "snippet/": 1, "flow/": 2, "alert/": 3, "script/": 4, "configmap/": 4, "settings/": 4}
	changes := append([]ConfigChange{}, plan.Changes...)
	rank := func(c ConfigChange) int {
		for prefix, n := range order {
			if strings.HasPrefix(c.Key, prefix) {
				if c.Action == "remove" {
					return 10 - n // removals after, in reverse (libraries last)
				}
				return n
			}
		}
		return 20
	}
	sort.SliceStable(changes, func(i, j int) bool { return rank(changes[i]) < rank(changes[j]) })
	var steps []configStep
	for _, c := range changes {
		st, err := s.stepFor(c)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c.Key, err)
		}
		steps = append(steps, st)
	}
	return steps, nil
}

// stepFor builds the write for one change and the write that undoes it.
func (s *Server) stepFor(c ConfigChange) (configStep, error) {
	kind, name, _ := strings.Cut(c.Key, "/")
	put := func(v json.RawMessage) func(context.Context) error { return s.putArtifact(kind, name, v) }
	del := func(ctx context.Context) error { return s.deleteArtifact(ctx, kind, name) }
	st := configStep{key: c.Key}
	switch c.Action {
	case "add":
		st.do, st.undo = put(c.After), del
	case "update":
		st.do, st.undo = put(c.After), put(c.Before)
	case "remove":
		st.do, st.undo, st.removeFlow = del, put(c.Before), kind == "flow"
	default:
		return st, fmt.Errorf("unknown action %q", c.Action)
	}
	return st, nil
}

// putArtifact creates or replaces one artifact from its JSON.
func (s *Server) putArtifact(kind, name string, v json.RawMessage) func(context.Context) error {
	return func(ctx context.Context) error {
		switch kind {
		case "flow":
			var f Flow
			if err := json.Unmarshal(v, &f); err != nil {
				return err
			}
			_, err := s.cfg.Transfer.Import(ctx, []Flow{f}, true)
			return err
		case "alert":
			var a Alert
			if err := json.Unmarshal(v, &a); err != nil {
				return err
			}
			return s.cfg.Alerts.SaveAlerts(ctx, []Alert{a}, false)
		case "snippet":
			var sn Snippet
			if err := json.Unmarshal(v, &sn); err != nil {
				return err
			}
			return s.cfg.Snippets.SaveSnippets(ctx, []Snippet{sn}, false)
		case "library":
			var l SnippetLibrary
			if err := json.Unmarshal(v, &l); err != nil {
				return err
			}
			return s.cfg.Snippets.SaveLibraries(ctx, []SnippetLibrary{l}, false)
		case "script", "configmap", "settings":
			return s.cfg.Items.PutItem(ctx, itemKindOf[kind], name, v)
		}
		return fmt.Errorf("unknown artifact kind %q", kind)
	}
}

// itemKindOf maps a plan's artifact kind to its item store kind.
var itemKindOf = map[string]string{"script": "scripts", "configmap": "configmap", "settings": "settings"}

// deleteArtifact removes one artifact.
func (s *Server) deleteArtifact(ctx context.Context, kind, name string) error {
	switch kind {
	case "flow":
		return s.cfg.Flows.Delete(ctx, name)
	case "alert":
		return s.cfg.Alerts.DeleteAlert(ctx, name)
	case "snippet":
		return s.cfg.Snippets.DeleteSnippet(ctx, name)
	case "library":
		return s.cfg.Snippets.DeleteLibrary(ctx, name)
	case "script", "configmap", "settings":
		return s.cfg.Items.DeleteItem(ctx, itemKindOf[kind], name)
	}
	return fmt.Errorf("unknown artifact kind %q", kind)
}

// auditApply adds the plan and result to the request's audit record.
func (s *Server) auditApply(r *http.Request, plan ConfigPlan, result string) {
	info := auditInfoFrom(r.Context())
	if info.detail == nil {
		info.detail = map[string]string{}
	}
	info.detail["result"] = result
	info.detail["plan.fingerprint"] = plan.Fingerprint
	for name, keys := range map[string][]string{"plan.added": plan.Added, "plan.updated": plan.Updated, "plan.removed": plan.Removed} {
		info.detail[name] = strconv.Itoa(len(keys))
		if len(keys) > 0 {
			info.detail[name] += ": " + strings.Join(firstN(keys, 50), ",")
		}
	}
}

// firstN returns at most n keys, noting how many more there are.
func firstN(keys []string, n int) []string {
	if len(keys) <= n {
		return keys
	}
	return append(append([]string{}, keys[:n]...), fmt.Sprintf("… %d more", len(keys)-n))
}
