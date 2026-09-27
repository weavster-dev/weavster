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
	key      string
	do, undo func(context.Context) error
	// restores names a flow that undo brings back undeployed.
	restores string
}

// applyOrder ranks artifact kinds: writes run in this order, removals after
// them in the reverse order (snippets before their library, flows before
// the flows they use).
var applyOrder = map[string]int{"library": 0, "snippet": 1, "flow": 2, "alert": 3, "script": 4, "configmap": 4, "settings": 4}

// handleConfigApply applies a config-as-code document (spec D-04, #107
// D-48): it plans again, refuses a fingerprint that no longer matches the
// live configuration and the document, applies the changes, and undoes
// every applied change when one fails. Applies are serialized; the audit
// record carries the plan, reason, and result of every attempt.
func (s *Server) handleConfigApply(w http.ResponseWriter, r *http.Request) {
	refuse := func(status int, result, msg string) {
		s.auditApply(r, ConfigPlan{}, result)
		writeStatusError(w, status, msg)
	}
	if s.cfg.ConfigPlanner == nil || s.cfg.Flows == nil {
		refuse(http.StatusServiceUnavailable, "unavailable", "configuration apply unavailable")
		return
	}
	if !s.configPorts(w, false, false) {
		s.auditApply(r, ConfigPlan{}, "unavailable")
		return
	}
	opts, ok := boolParams(w, r, "dryRun")
	if !ok {
		s.auditApply(r, ConfigPlan{}, "refused")
		return
	}
	q := r.URL.Query()
	fingerprint, reason := q.Get("fingerprint"), q.Get("reason")
	if fingerprint == "" {
		refuse(http.StatusBadRequest, "refused", "fingerprint is required: plan first (POST /api/v1/config/plan) and pass the plan's fingerprint")
		return
	}
	if utf8.RuneCountInString(reason) > 500 {
		refuse(http.StatusBadRequest, "refused", "reason must be at most 500 characters")
		return
	}
	doc, ok := readConfigBody(w, r)
	if !ok {
		s.auditApply(r, ConfigPlan{}, "refused")
		return
	}
	s.applyMu.Lock()
	defer s.applyMu.Unlock()
	ctx := r.Context()
	live, err := s.liveConfig(ctx, true)
	var plan ConfigPlan
	if err == nil {
		plan, err = s.cfg.ConfigPlanner.PlanConfig(doc, live)
	}
	var steps []configStep
	if err == nil {
		steps, err = s.applySteps(plan)
	}
	switch {
	case errors.Is(err, ErrInvalidConfig):
		s.auditApply(r, plan, "invalid")
		writeStatusError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		s.auditApply(r, plan, "failed")
		writeBackendError(w, err)
		return
	case plan.Fingerprint != fingerprint:
		s.auditApply(r, plan, "stale")
		writeStatusError(w, http.StatusConflict, "the configuration or the document changed since the plan was made; plan again and review the new plan")
		return
	case opts["dryRun"]:
		s.auditApply(r, plan, "dry run")
		writeJSON(w, http.StatusOK, map[string]any{"applied": false, "plan": plan})
		return
	}
	// Writes finish even if the client goes away, so nothing is left half done.
	failed, undoFailed, restored, err := runSteps(context.WithoutCancel(ctx), steps)
	if err == nil {
		s.auditApply(r, plan, "applied")
		writeJSON(w, http.StatusOK, map[string]any{"applied": true, "plan": plan})
		return
	}
	result, status := "rolled back", http.StatusConflict
	msg := fmt.Sprintf("apply stopped at %s: %v; every change was rolled back", failed, err)
	if !isConflict(err) {
		status = http.StatusInternalServerError
		msg = fmt.Sprintf("apply stopped at %s because of an internal error; every change was rolled back", failed)
	}
	if len(undoFailed) > 0 {
		result, status = "rollback failed", http.StatusInternalServerError
		msg = fmt.Sprintf("apply stopped at %s, and undoing %s failed: the configuration is partly applied; plan again to see where it stands",
			failed, strings.Join(undoFailed, ", "))
	}
	if len(restored) > 0 {
		msg += "; restored flows are undeployed, deploy them again if they were running: " + strings.Join(restored, ", ")
	}
	s.auditApply(r, plan, result)
	writeStatusError(w, status, msg)
}

// isConflict reports errors that come from the configuration itself (a
// flow still in use, an id taken meanwhile) rather than the server. Apply
// spans every resource, so it gathers the 409 and 404 errors of all of them.
func isConflict(err error) bool {
	for _, e := range []error{ErrFlowInUse, ErrImportConflict, ErrInvalidFlow, ErrAlertExists, ErrSnippetExists, ErrLibraryExists,
		ErrLibraryInUse, ErrLibraryNotFound, ErrFlowNotFound, ErrAlertNotFound, ErrSnippetNotFound, ErrItemNotFound} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// runSteps runs the steps in order. When one fails it undoes every applied
// step in reverse, carrying on past undo failures, and returns the failed
// key, the keys it could not undo, the flows undo brought back
// (undeployed), and the error.
func runSteps(ctx context.Context, steps []configStep) (failed string, undoFailed, restored []string, err error) {
	for i, st := range steps {
		if err = st.do(ctx); err == nil {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			if uerr := steps[j].undo(ctx); uerr != nil {
				undoFailed = append(undoFailed, steps[j].key)
			} else if steps[j].restores != "" {
				restored = append(restored, steps[j].restores)
			}
		}
		return st.key, undoFailed, restored, err
	}
	return "", nil, nil, nil
}

// applySteps turns the plan's changes into ordered writes with their undo:
// every flow add and update in one import (which orders flows by their
// dependencies), and flow removals dependents-first. An unknown artifact
// kind is refused before anything is written.
func (s *Server) applySteps(plan ConfigPlan) ([]configStep, error) {
	type ranked struct {
		rank int
		step configStep
	}
	var out []ranked
	var flowPuts, flowRemovals []ConfigChange
	for _, c := range plan.Changes {
		kind, _, _ := strings.Cut(c.Key, "/")
		n, ok := applyOrder[kind]
		if !ok {
			return nil, fmt.Errorf("%w: unknown artifact kind in %s", ErrInvalidConfig, c.Key)
		}
		switch {
		case kind == "flow" && c.Action == "remove":
			flowRemovals = append(flowRemovals, c)
		case kind == "flow":
			flowPuts = append(flowPuts, c)
		case c.Action == "remove":
			out = append(out, ranked{10 - n, s.stepFor(c)})
		default:
			out = append(out, ranked{n, s.stepFor(c)})
		}
	}
	if len(flowPuts) > 0 {
		st, err := s.flowPutStep(flowPuts)
		if err != nil {
			return nil, err
		}
		out = append(out, ranked{applyOrder["flow"], st})
	}
	removals, err := removalOrder(flowRemovals)
	if err != nil {
		return nil, err
	}
	for _, c := range removals {
		st := s.stepFor(c)
		st.restores = strings.TrimPrefix(c.Key, "flow/")
		out = append(out, ranked{10 - applyOrder["flow"], st})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].rank < out[j].rank })
	steps := make([]configStep, len(out))
	for i, r := range out {
		steps[i] = r.step
	}
	return steps, nil
}

// flowPutStep imports every added and updated flow at once; its undo
// deletes the added ones and puts the updated ones back.
func (s *Server) flowPutStep(changes []ConfigChange) (configStep, error) {
	var after, before []Flow
	var added, keys []string
	for _, c := range changes {
		var f Flow
		if err := json.Unmarshal(c.After, &f); err != nil {
			return configStep{}, fmt.Errorf("%s: %w", c.Key, err)
		}
		after = append(after, f)
		keys = append(keys, c.Key)
		if c.Action == "add" {
			added = append(added, f.ID)
			continue
		}
		var b Flow
		if err := json.Unmarshal(c.Before, &b); err != nil {
			return configStep{}, fmt.Errorf("%s: %w", c.Key, err)
		}
		before = append(before, b)
	}
	return configStep{
		key: strings.Join(keys, ", "),
		do: func(ctx context.Context) error {
			_, err := s.cfg.Transfer.Import(ctx, after, true)
			return err
		},
		undo: func(ctx context.Context) error {
			for _, id := range added {
				if err := s.cfg.Flows.Delete(ctx, id); err != nil && !errors.Is(err, ErrFlowNotFound) {
					return err
				}
			}
			if len(before) == 0 {
				return nil
			}
			_, err := s.cfg.Transfer.Import(ctx, before, true)
			return err
		},
	}, nil
}

// removalOrder orders flow removals so a flow goes before the flows it
// depends on (from the removed flows' own dependsOn).
func removalOrder(changes []ConfigChange) ([]ConfigChange, error) {
	deps := map[string][]string{}
	byID := map[string]ConfigChange{}
	for _, c := range changes {
		var f Flow
		if err := json.Unmarshal(c.Before, &f); err != nil {
			return nil, fmt.Errorf("%s: %w", c.Key, err)
		}
		id := strings.TrimPrefix(c.Key, "flow/")
		deps[id], byID[id] = f.DependsOn, c
	}
	usedBy := map[string]int{} // removed flows that still depend on each flow
	for _, ds := range deps {
		for _, d := range ds {
			if _, ok := byID[d]; ok {
				usedBy[d]++
			}
		}
	}
	var order []ConfigChange
	for len(byID) > 0 {
		var ready []string
		for id := range byID {
			if usedBy[id] == 0 {
				ready = append(ready, id)
			}
		}
		if len(ready) == 0 { // a cycle cannot be stored; keep a stable order anyway
			for id := range byID {
				ready = append(ready, id)
			}
		}
		sort.Strings(ready)
		for _, id := range ready {
			order = append(order, byID[id])
			delete(byID, id)
			for _, d := range deps[id] {
				usedBy[d]--
			}
		}
	}
	return order, nil
}

// stepFor builds the write for one non-flow change, or a flow removal, and
// the write that undoes it.
func (s *Server) stepFor(c ConfigChange) configStep {
	kind, name, _ := strings.Cut(c.Key, "/")
	put := func(v json.RawMessage) func(context.Context) error { return s.putArtifact(kind, name, v) }
	del := func(ctx context.Context) error { return s.deleteArtifact(ctx, kind, name) }
	st := configStep{key: c.Key}
	switch c.Action {
	case "add":
		st.do, st.undo = put(c.After), del
	case "update":
		st.do, st.undo = put(c.After), put(c.Before)
	default: // remove
		st.do, st.undo = del, put(c.Before)
	}
	return st
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
		}
		return s.cfg.Items.PutItem(ctx, itemKindOf[kind], name, v)
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
	}
	return s.cfg.Items.DeleteItem(ctx, itemKindOf[kind], name)
}

// auditApply adds the plan and result to the request's audit record.
func (s *Server) auditApply(r *http.Request, plan ConfigPlan, result string) {
	info := auditInfoFrom(r.Context())
	if info.detail == nil {
		info.detail = map[string]string{}
	}
	info.detail["result"] = result
	if plan.Fingerprint == "" {
		return // refused before planning
	}
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
