package gateway

import (
	"context"
	"encoding/json"
	"errors"
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

// TestRunStepsRemovesDependentsFirst: a flow in use is removed after the
// flow using it; one that stays in use rolls everything back.
func TestRunStepsRemovesDependentsFirst(t *testing.T) {
	removed := map[string]bool{}
	var undone []string
	flowStep := func(id, usedBy string) configStep {
		return configStep{key: "flow/" + id, removeFlow: true,
			do: func(context.Context) error {
				if usedBy != "" && !removed[usedBy] {
					return ErrFlowInUse
				}
				removed[id] = true
				return nil
			},
			undo: func(context.Context) error { undone = append(undone, id); return nil }}
	}
	if failed, err := runSteps(context.Background(), []configStep{flowStep("base", "app"), flowStep("app", "")}); err != nil {
		t.Fatalf("runSteps = %s %v", failed, err)
	}
	if !removed["base"] || !removed["app"] {
		t.Errorf("removed = %v", removed)
	}
	failed, err := runSteps(context.Background(), []configStep{flowStep("x", "kept"), flowStep("y", "")})
	if failed != "flow/x" || !errors.Is(err, ErrFlowInUse) || strings.Join(undone, ",") != "y" {
		t.Errorf("stuck removal = %s %v, undone %v", failed, err, undone)
	}
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
