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
)

// ConfigFormat names the full-configuration document (spec §2.9).
const ConfigFormat = "weavster-config-v1"

// ConfigBundle is the full configuration: every flow, alert, snippet,
// snippet library, global script, and setting, and optionally the config
// map. Users are never included.
type ConfigBundle struct {
	Format           string                     `json:"format"`
	Flows            []Flow                     `json:"flows"`
	Alerts           []Alert                    `json:"alerts"`
	Snippets         []Snippet                  `json:"snippets"`
	SnippetLibraries []SnippetLibrary           `json:"snippetLibraries"`
	Scripts          map[string]json.RawMessage `json:"scripts"`
	Settings         map[string]json.RawMessage `json:"settings"`
	// ConfigMap is nil unless asked for; an empty map is exported as {}.
	ConfigMap *map[string]json.RawMessage `json:"configmap,omitempty"`
}

// ConfigImportResult reports what a configuration import wrote.
type ConfigImportResult struct {
	Flows            ImportResult `json:"flows"`
	Alerts           int          `json:"alerts"`
	Snippets         int          `json:"snippets"`
	SnippetLibraries int          `json:"snippetLibraries"`
	Scripts          int          `json:"scripts"`
	Settings         int          `json:"settings"`
	ConfigMap        bool         `json:"configMapReplaced"`
	Deployed         []string     `json:"deployed"`
}

// configPorts reports whether every port a configuration transfer uses is
// wired, answering 503 when one is not.
func (s *Server) configPorts(w http.ResponseWriter) bool {
	if s.cfg.Transfer == nil || s.cfg.Flows == nil || s.cfg.Lifecycle == nil || s.cfg.Alerts == nil || s.cfg.Snippets == nil || s.cfg.Items == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "configuration export and import unavailable")
		return false
	}
	return true
}

// boolParams reads optional true/false query parameters in order; on a bad
// value it answers 400 and returns false.
func boolParams(w http.ResponseWriter, r *http.Request, names ...string) (map[string]bool, bool) {
	out := map[string]bool{}
	for _, n := range names {
		if v := r.URL.Query().Get(n); v != "" {
			b, err := strconv.ParseBool(v)
			if err != nil {
				writeStatusError(w, http.StatusBadRequest, n+" must be true or false")
				return nil, false
			}
			out[n] = b
		}
	}
	return out, true
}

func (s *Server) handleConfigExport(w http.ResponseWriter, r *http.Request) {
	if !s.configPorts(w) {
		return
	}
	opts, ok := boolParams(w, r, "includeConfigMap")
	if !ok {
		return
	}
	ctx := r.Context()
	b := ConfigBundle{Format: ConfigFormat}
	var err error
	if b.Flows, err = s.cfg.Transfer.Export(ctx, nil); err == nil {
		b.Alerts, err = s.cfg.Alerts.ListAlerts(ctx)
	}
	if err == nil {
		b.Snippets, err = s.cfg.Snippets.ListSnippets(ctx)
	}
	if err == nil {
		b.SnippetLibraries, err = s.cfg.Snippets.ListLibraries(ctx)
	}
	if err == nil {
		b.Scripts, err = s.cfg.Items.ListItems(ctx, "scripts")
	}
	if err == nil {
		b.Settings, err = s.cfg.Items.ListItems(ctx, "settings")
	}
	if err == nil && opts["includeConfigMap"] {
		var m map[string]json.RawMessage
		m, err = s.cfg.Items.ListItems(ctx, "configmap")
		b.ConfigMap = &m
	}
	// Empty sets are written as {}, never null.
	for _, m := range []*map[string]json.RawMessage{&b.Scripts, &b.Settings, b.ConfigMap} {
		if m != nil && *m == nil {
			*m = map[string]json.RawMessage{}
		}
	}
	if err != nil {
		writeBackendError(w, err)
		return
	}
	sort.Slice(b.Alerts, func(i, j int) bool { return b.Alerts[i].ID < b.Alerts[j].ID })
	sort.Slice(b.Snippets, func(i, j int) bool { return b.Snippets[i].Name < b.Snippets[j].Name })
	sort.Slice(b.SnippetLibraries, func(i, j int) bool { return b.SnippetLibraries[i].Name < b.SnippetLibraries[j].Name })
	writeJSON(w, http.StatusOK, b)
}

// configDocument is ConfigBundle as read: flows stay raw so they are
// validated like any flow import.
type configDocument struct {
	Format           string                     `json:"format"`
	Flows            []json.RawMessage          `json:"flows"`
	Alerts           []Alert                    `json:"alerts"`
	Snippets         []Snippet                  `json:"snippets"`
	SnippetLibraries []SnippetLibrary           `json:"snippetLibraries"`
	Scripts          map[string]json.RawMessage `json:"scripts"`
	Settings         map[string]json.RawMessage `json:"settings"`
	ConfigMap        map[string]json.RawMessage `json:"configmap"`
}

// readConfigDocument decodes a configuration document of at most
// maxImportBytes, rejecting unknown fields.
func readConfigDocument(w http.ResponseWriter, r *http.Request) (configDocument, bool) {
	var doc configDocument
	if !readStrictJSON(w, r, maxImportBytes, &doc) {
		return doc, false
	}
	if doc.Format != ConfigFormat {
		writeStatusError(w, http.StatusBadRequest, fmt.Sprintf("format must be %q, as exported by GET /api/v1/config/export", ConfigFormat))
		return doc, false
	}
	return doc, true
}

// checkConfigDocument validates every part of doc; the error is safe to show.
// The config map is checked only when it will be written (withConfigMap).
func checkConfigDocument(doc configDocument, withConfigMap bool) error {
	if err := checkNames[SnippetLibrary](doc.SnippetLibraries, "library"); err != nil {
		return err
	}
	libs := map[string]bool{}
	for _, l := range doc.SnippetLibraries {
		libs[l.Name] = true
	}
	if err := checkNames[Snippet](doc.Snippets, "snippet"); err != nil {
		return err
	}
	for _, sn := range doc.Snippets {
		if sn.Library != "" && !libs[sn.Library] {
			if err := checkName(sn.Library); err != nil {
				return fmt.Errorf("snippet %s: library: %w", sn.Name, err)
			}
		}
	}
	if err := checkNames[Alert](doc.Alerts, "alert"); err != nil {
		return err
	}
	for _, a := range doc.Alerts {
		if err := checkAlert(a); err != nil {
			return err
		}
	}
	parts := map[string]map[string]json.RawMessage{"scripts": doc.Scripts, "settings": doc.Settings}
	if withConfigMap {
		parts["configmap"] = doc.ConfigMap
	}
	for _, k := range itemKinds {
		for name, v := range parts[k.kind] {
			if err := k.checkItem(name, v); err != nil {
				return fmt.Errorf("%s: %w", k.kind, err)
			}
		}
	}
	return nil
}

// configConflicts lists the flows, alerts, snippets, and libraries in doc
// that already exist.
func (s *Server) configConflicts(ctx context.Context, doc configDocument, flows []Flow) ([]string, error) {
	var out []string
	existing, err := s.cfg.Flows.List(ctx)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, f := range existing {
		have["flow "+f.ID] = true
	}
	alerts, err := s.cfg.Alerts.ListAlerts(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range alerts {
		have["alert "+a.ID] = true
	}
	snippets, err := s.cfg.Snippets.ListSnippets(ctx)
	if err != nil {
		return nil, err
	}
	for _, sn := range snippets {
		have["snippet "+sn.Name] = true
	}
	libs, err := s.cfg.Snippets.ListLibraries(ctx)
	if err != nil {
		return nil, err
	}
	for _, l := range libs {
		have["library "+l.Name] = true
	}
	var keys []string
	for _, f := range flows {
		keys = append(keys, "flow "+f.ID)
	}
	for _, a := range doc.Alerts {
		keys = append(keys, "alert "+a.ID)
	}
	for _, sn := range doc.Snippets {
		keys = append(keys, "snippet "+sn.Name)
	}
	for _, l := range doc.SnippetLibraries {
		keys = append(keys, "library "+l.Name)
	}
	for _, k := range keys {
		if have[k] {
			out = append(out, k)
		}
	}
	return out, nil
}

// handleConfigImport restores a configuration document (spec §2.9.30–31):
// everything is validated and checked for conflicts first; then flows,
// libraries, snippets, alerts, scripts, settings, and the config map
// (overwriteConfigMap only) are written, and imported flows are deployed
// unless nodeploy.
func (s *Server) handleConfigImport(w http.ResponseWriter, r *http.Request) {
	if !s.configPorts(w) {
		return
	}
	opts, ok := boolParams(w, r, "force", "nodeploy", "overwriteConfigMap")
	if !ok {
		return
	}
	doc, ok := readConfigDocument(w, r)
	if !ok {
		return
	}
	flows, _, ok := decodeFlowList(w, doc.Flows)
	if !ok {
		return
	}
	if err := checkConfigDocument(doc, opts["overwriteConfigMap"]); err != nil {
		writeStatusError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	if missing, err := s.missingLibraries(ctx, doc); err != nil {
		writeBackendError(w, err)
		return
	} else if len(missing) > 0 {
		writeStatusError(w, http.StatusBadRequest, "snippets name libraries that are neither in the document nor on the server: "+strings.Join(missing, ", "))
		return
	}
	if !opts["force"] {
		conflicts, err := s.configConflicts(ctx, doc, flows)
		if err != nil {
			writeBackendError(w, err)
			return
		}
		if len(conflicts) > 0 {
			writeStatusError(w, http.StatusConflict, "already exist: "+strings.Join(conflicts, ", ")+" (use force=true to replace them)")
			return
		}
	}
	res := ConfigImportResult{Flows: ImportResult{Created: []string{}, Updated: []string{}}, Deployed: []string{}}
	// Without force, the stores refuse ids created since the conflict check
	// (create), so nothing that appeared meanwhile is overwritten.
	create := !opts["force"]
	// Flows first: their import validates every flow (dependencies
	// included) before writing any, and the other parts are checked above.
	var err error
	if len(flows) > 0 {
		res.Flows, err = s.cfg.Transfer.Import(ctx, flows, opts["force"])
	}
	if err == nil {
		if err = s.cfg.Snippets.SaveLibraries(ctx, doc.SnippetLibraries, create); err == nil {
			res.SnippetLibraries = len(doc.SnippetLibraries)
		}
	}
	if err == nil {
		if err = s.cfg.Snippets.SaveSnippets(ctx, doc.Snippets, create); err == nil {
			res.Snippets = len(doc.Snippets)
		}
	}
	if err == nil {
		if err = s.cfg.Alerts.SaveAlerts(ctx, doc.Alerts, create); err == nil {
			res.Alerts = len(doc.Alerts)
		}
	}
	if err == nil {
		err = s.putItems(ctx, "scripts", doc.Scripts, &res.Scripts)
	}
	if err == nil {
		err = s.putItems(ctx, "settings", doc.Settings, &res.Settings)
	}
	if err == nil && opts["overwriteConfigMap"] && doc.ConfigMap != nil {
		if err = s.cfg.Items.ReplaceItems(ctx, "configmap", doc.ConfigMap); err == nil {
			res.ConfigMap = true
		}
	}
	if err != nil {
		writeConfigImportError(w, err)
		return
	}
	if !opts["nodeploy"] {
		if res.Deployed, err = s.deployImported(ctx, flows); err != nil {
			writeStatusError(w, http.StatusInternalServerError, "the configuration was imported, but "+err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, res)
}

// deployImported deploys the imported flows that are enabled and
// undeployed, and returns every imported flow that went from undeployed to
// deployed, including dependencies a deploy took along.
func (s *Server) deployImported(ctx context.Context, flows []Flow) ([]string, error) {
	wasUndeployed := map[string]bool{}
	for _, f := range flows {
		cur, err := s.cfg.Flows.Get(ctx, f.ID)
		if err != nil {
			return nil, fmt.Errorf("flow %s could not be read: %w", f.ID, err)
		}
		wasUndeployed[f.ID] = cur.Status == "undeployed"
	}
	deployed := []string{}
	for _, f := range flows {
		// The stored flow says whether it is enabled (the document may leave
		// it to the default), and whether an earlier deploy took it along.
		cur, err := s.cfg.Flows.Get(ctx, f.ID)
		if err != nil {
			return deployed, fmt.Errorf("flow %s could not be read: %w", f.ID, err)
		}
		if cur.Enabled && cur.Status == "undeployed" {
			if _, err := s.cfg.Lifecycle.Transition(ctx, f.ID, "deploy"); err != nil {
				return deployed, fmt.Errorf("flow %s did not deploy: %w; deploy it with POST /api/v1/flows/%s/deploy", f.ID, err, f.ID)
			}
		}
	}
	for _, f := range flows {
		if !wasUndeployed[f.ID] {
			continue
		}
		if cur, err := s.cfg.Flows.Get(ctx, f.ID); err == nil && cur.Status != "undeployed" {
			deployed = append(deployed, f.ID)
		}
	}
	return deployed, nil
}

// missingLibraries lists the libraries doc's snippets name that neither doc
// nor the server has.
func (s *Server) missingLibraries(ctx context.Context, doc configDocument) ([]string, error) {
	have := map[string]bool{"": true}
	for _, l := range doc.SnippetLibraries {
		have[l.Name] = true
	}
	var missing []string
	for _, sn := range doc.Snippets {
		if have[sn.Library] {
			continue
		}
		if _, err := s.cfg.Snippets.GetLibrary(ctx, sn.Library); errors.Is(err, ErrLibraryNotFound) {
			missing = append(missing, sn.Library)
		} else if err != nil {
			return nil, err
		}
		have[sn.Library] = true
	}
	return missing, nil
}

// putItems creates or replaces each item of kind, counting them in n.
func (s *Server) putItems(ctx context.Context, kind string, items map[string]json.RawMessage, n *int) error {
	names := make([]string, 0, len(items))
	for name := range items {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := s.cfg.Items.PutItem(ctx, kind, name, items[name]); err != nil {
			return err
		}
		*n++
	}
	return nil
}

// writeConfigImportError reports a failed write. Invalid flows and flow
// conflicts come before anything is written; the rest are changes made
// meanwhile by others, or a part-way failure.
func writeConfigImportError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidFlow):
		writeFlowError(w, err)
	case errors.Is(err, ErrImportConflict), errors.Is(err, ErrAlertExists), errors.Is(err, ErrSnippetExists), errors.Is(err, ErrLibraryExists):
		writeStatusError(w, http.StatusConflict, "an item in the document was created on the server during the import; import again, with force=true to replace it")
	case errors.Is(err, ErrLibraryNotFound):
		writeStatusError(w, http.StatusConflict, "a snippet library the document's snippets use was deleted during the import; import again")
	default:
		writeStatusError(w, http.StatusInternalServerError, "import stopped part-way because of an internal error; fix the cause and import again with force=true")
	}
}
