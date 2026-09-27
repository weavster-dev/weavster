package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// planOf is a planner returning a fixed plan (fingerprint "fp") for any
// document except "bad".
type planOf struct{ plan ConfigPlan }

func (p planOf) PlanConfig(doc []byte, _ ConfigBundle) (ConfigPlan, error) {
	if string(doc) == "bad" {
		return ConfigPlan{}, ErrInvalidConfig
	}
	return p.plan, nil
}

// undeletableAlerts fails every alert deletion (so a rollback fails).
type undeletableAlerts struct{ memAlerts }

func (*undeletableAlerts) DeleteAlert(context.Context, string) error { return errDisk }

var testPlan = ConfigPlan{Fingerprint: "fp", Changes: []ConfigChange{
	{Key: "settings/k", Action: "remove", Before: json.RawMessage(`1`)},
	{Key: "script/s", Action: "update", Before: json.RawMessage(`"a"`), After: json.RawMessage(`"b"`)},
	{Key: "alert/errors", Action: "add", After: json.RawMessage(validAlert)},
}}

func TestConfigApplyHandler(t *testing.T) {
	type state struct {
		items  memItems
		alerts *memAlerts
	}
	setup := func(withSetting bool) (Config, state) {
		st := state{items: memItems{"scripts": {"s": json.RawMessage(`"a"`)}, "settings": {}}, alerts: &memAlerts{alerts: map[string]Alert{}}}
		if withSetting {
			st.items["settings"]["k"] = json.RawMessage(`1`)
		}
		return Config{ConfigPlanner: planOf{testPlan}, Transfer: fakeTransfer{}, Flows: &fakeFlows{}, Alerts: st.alerts,
			Snippets: &memSnippets{snippets: map[string]Snippet{}, libraries: map[string]SnippetLibrary{}}, Items: st.items}, st
	}
	do := func(cfg Config, query, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		New(cfg).Router().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/config/apply"+query, strings.NewReader(body)))
		return rec
	}

	cfg, st := setup(true)
	if rec := do(cfg, "?fingerprint=fp", "doc"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"applied":true`) {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body.String())
	}
	if string(st.items["scripts"]["s"]) != `"b"` || len(st.items["settings"]) != 0 || len(st.alerts.alerts) != 1 {
		t.Errorf("after apply: items %v, alerts %v", st.items, st.alerts.alerts)
	}

	// The setting to remove is missing: the removal fails after the alert and
	// script were written, and both are undone.
	cfg, st = setup(false)
	if rec := do(cfg, "?fingerprint=fp", "doc"); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "apply stopped at settings/k") || !strings.Contains(rec.Body.String(), "rolled back") {
		t.Errorf("failed apply: %d %s", rec.Code, rec.Body.String())
	}
	if string(st.items["scripts"]["s"]) != `"a"` || len(st.alerts.alerts) != 0 {
		t.Errorf("not rolled back: items %v, alerts %v", st.items, st.alerts.alerts)
	}

	// Undoing the alert fails: the reply says the configuration is partly applied.
	cfg, _ = setup(false)
	cfg.Alerts = &undeletableAlerts{memAlerts{alerts: map[string]Alert{}}}
	if rec := do(cfg, "?fingerprint=fp", "doc"); rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "undoing alert/errors failed") {
		t.Errorf("rollback failure: %d %s", rec.Code, rec.Body.String())
	}

	cfg, st = setup(true)
	for _, tt := range []struct {
		name, query, body string
		status            int
		want              string
	}{
		{"no fingerprint", "", "doc", http.StatusBadRequest, "fingerprint is required"},
		{"stale", "?fingerprint=old", "doc", http.StatusConflict, "changed since the plan"},
		{"dry run", "?fingerprint=fp&dryRun=true", "doc", http.StatusOK, `"applied":false`},
		{"invalid", "?fingerprint=fp", "bad", http.StatusBadRequest, "invalid configuration document"},
		{"bad flag", "?fingerprint=fp&dryRun=x", "doc", http.StatusBadRequest, "dryRun must be"},
		{"long reason", "?fingerprint=fp&reason=" + strings.Repeat("x", 501), "doc", http.StatusBadRequest, "at most 500"},
	} {
		if rec := do(cfg, tt.query, tt.body); rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.want) {
			t.Errorf("%s: %d %s", tt.name, rec.Code, rec.Body.String())
		}
	}
	if string(st.items["scripts"]["s"]) != `"a"` {
		t.Error("a dry run or refused apply changed something")
	}
	if rec := do(Config{}, "?fingerprint=fp", "doc"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("unavailable: %d", rec.Code)
	}
	cfg, _ = setup(true)
	cfg.Transfer = fakeTransfer{err: errDisk}
	if rec := do(cfg, "?fingerprint=fp", "doc"); rec.Code != http.StatusInternalServerError {
		t.Errorf("live read fails: %d", rec.Code)
	}
}

// TestRemovalOrder: a removed flow goes before the removed flows it uses.
func TestRemovalOrder(t *testing.T) {
	change := func(id string, deps ...string) ConfigChange {
		before, _ := json.Marshal(Flow{ID: id, DependsOn: deps})
		return ConfigChange{Key: "flow/" + id, Action: "remove", Before: before}
	}
	got, err := removalOrder([]ConfigChange{change("base"), change("mid", "base"), change("app", "mid", "kept")})
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, c := range got {
		keys = append(keys, c.Key)
	}
	if strings.Join(keys, ",") != "flow/app,flow/mid,flow/base" {
		t.Errorf("order = %v", keys)
	}
	if _, err := removalOrder([]ConfigChange{{Key: "flow/x", Before: json.RawMessage(`[`)}}); err == nil {
		t.Error("bad before: no error")
	}
}

// TestRunStepsUndoesPastFailures: undo continues after an undo fails, and
// reports both the failures and the restored flows.
func TestRunStepsUndoesPastFailures(t *testing.T) {
	var undone []string
	step := func(key string, doErr, undoErr error, restores string) configStep {
		return configStep{key: key, restores: restores,
			do:   func(context.Context) error { return doErr },
			undo: func(context.Context) error { undone = append(undone, key); return undoErr }}
	}
	failed, undoFailed, restored, err := runSteps(context.Background(), []configStep{
		step("a", nil, nil, ""), step("b", nil, errDisk, ""), step("flow/c", nil, nil, "c"), step("d", errDisk, nil, ""),
	})
	if failed != "d" || !errors.Is(err, errDisk) || strings.Join(undone, ",") != "flow/c,b,a" ||
		strings.Join(undoFailed, ",") != "b" || strings.Join(restored, ",") != "c" {
		t.Errorf("runSteps = %s %v; undone %v, undo failed %v, restored %v", failed, err, undone, undoFailed, restored)
	}
}

// TestFlowPutStep: every added and updated flow is imported in one call,
// and undo deletes the added ones and re-imports the old versions.
func TestFlowPutStep(t *testing.T) {
	var imported [][]string
	flows := &deletingFlows{}
	s := New(Config{Transfer: recordingTransfer{&imported}, Flows: flows})
	st, err := s.flowPutStep([]ConfigChange{
		{Key: "flow/new", Action: "add", After: json.RawMessage(`{"id":"new"}`)},
		{Key: "flow/old", Action: "update", Before: json.RawMessage(`{"id":"old","name":"A"}`), After: json.RawMessage(`{"id":"old","name":"B"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.do(context.Background()); err != nil || fmt.Sprint(imported) != "[[new old]]" {
		t.Fatalf("do: %v, imported %v", err, imported)
	}
	if err := st.undo(context.Background()); err != nil || fmt.Sprint(imported) != "[[new old] [old]]" || fmt.Sprint(flows.deleted) != "[new]" {
		t.Errorf("undo: %v, imported %v, deleted %v", err, imported, flows.deleted)
	}
	if _, err := s.flowPutStep([]ConfigChange{{Key: "flow/x", Action: "add", After: json.RawMessage(`[`)}}); err == nil {
		t.Error("bad after: no error")
	}
	if _, err := s.flowPutStep([]ConfigChange{{Key: "flow/x", Action: "update", After: json.RawMessage(`{}`), Before: json.RawMessage(`[`)}}); err == nil {
		t.Error("bad before: no error")
	}
	if _, err := s.applySteps(ConfigPlan{Changes: []ConfigChange{{Key: "widget/x", Action: "add"}}}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("unknown kind = %v", err)
	}
}

// deletingFlows records deleted flow ids.
type deletingFlows struct {
	fakeFlows
	deleted []string
}

func (d *deletingFlows) Delete(_ context.Context, id string) error {
	d.deleted = append(d.deleted, id)
	return nil
}

// recordingTransfer records the flow ids of every import.
type recordingTransfer struct{ imports *[][]string }

func (recordingTransfer) Export(context.Context, []string) ([]Flow, error) { return nil, nil }
func (r recordingTransfer) Import(_ context.Context, flows []Flow, _ bool) (ImportResult, error) {
	var ids []string
	for _, f := range flows {
		ids = append(ids, f.ID)
	}
	*r.imports = append(*r.imports, ids)
	return ImportResult{}, nil
}

func TestFirstN(t *testing.T) {
	keys := make([]string, 52)
	for i := range keys {
		keys[i] = "k"
	}
	if got := firstN(keys, 50); len(got) != 51 || got[50] != "… 2 more" {
		t.Errorf("firstN = %v", got[48:])
	}
}
