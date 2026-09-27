package config

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/artifact"
	"github.com/weavster-dev/weavster/internal/flowdef"
)

func TestLivePlan(t *testing.T) {
	live := &Config{
		Flows: map[string]flowdef.Flow{
			"adt":  {ID: "adt", Name: "ADT", Enabled: true, Transform: json.RawMessage(`{ "steps" : [ ] }`)},
			"same": {ID: "same", Name: "Same"},
			"gone": {ID: "gone"},
		},
		Alerts:    map[string]artifact.Alert{"kept": {ID: "kept", Name: "Kept"}},
		Settings:  map[string]any{"old": 1},
		Scripts:   map[string]string{"deploy": "old()"},
		ConfigMap: map[string]string{"region": "us"},
	}
	desired, err := Parse([]byte(`
flows:
  adt: {name: ADT Inbound, enabled: true, transform: {steps: []}, destinations: [{name: out, type: file, dir: /x}]}
  same: {name: Same}
  new: {name: New}
scripts:
  deploy: new()
configmap: {}
settings: {}
`))
	if err != nil {
		t.Fatal(err)
	}
	p := LivePlan(desired, live)
	if got := strings.Join(p.Added, ","); got != "flow/new" {
		t.Errorf("added = %s", got)
	}
	if got := strings.Join(p.Updated, ","); got != "flow/adt,script/deploy" {
		t.Errorf("updated = %s", got)
	}
	// alerts is not in the document, so alert/kept stays.
	if got := strings.Join(p.Removed, ","); got != "configmap/region,flow/gone,settings/old" {
		t.Errorf("removed = %s", got)
	}
	if p.Unchanged != 1 {
		t.Errorf("unchanged = %d, want 1 (flow/same; the transform differs only in formatting)", p.Unchanged)
	}
	text := p.Text()
	for _, want := range []string{
		"~ flow/adt\n    destinations: (none) → [{\"dir\":\"/x\",\"name\":\"out\",\"type\":\"file\"}]\n    name: \"ADT\" → \"ADT Inbound\"\n",
		"~ script/deploy\n    value: \"old()\" → \"new()\"\n",
		"+ flow/new\n", "- flow/gone\n",
		"1 to add, 2 to change, 3 to remove, 1 unchanged\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
	if (Plan{}).Text() != "no changes\n" {
		t.Error("empty plan text")
	}
	other, _ := Parse([]byte("flows: {}\n"))
	if p.Fingerprint == "" || p.Fingerprint != LivePlan(desired, live).Fingerprint || p.Fingerprint == LivePlan(other, live).Fingerprint {
		t.Error("the fingerprint must follow the live configuration and the document")
	}
	// A config built in code manages every section.
	if !(&Config{}).manages("alert/x") || desired.manages("unknown/x") {
		t.Error("manages")
	}
}

func TestFieldChangesArraysAndRemovals(t *testing.T) {
	got := fieldChanges(json.RawMessage(`{"a":[1,2],"b":{"c":1},"d":[1]}`), json.RawMessage(`{"a":[1,3],"b":{},"d":[1,2]}`))
	var paths []string
	for _, f := range got {
		paths = append(paths, f.Path+"="+string(f.Before)+">"+string(f.After))
	}
	if strings.Join(paths, " ") != "a[1]=2>3 b.c=1> d=[1]>[1,2]" {
		t.Errorf("changes = %v", paths)
	}
}

// TestLivePlanExactAndReadable: large integers compare exactly, code keeps
// < and &, and a section written without entries is managed.
func TestLivePlanExactAndReadable(t *testing.T) {
	live := &Config{
		Settings: map[string]any{"big": json.Number("9007199254740992")},
		Scripts:  map[string]string{"s": "if (a < b && c)"},
		Flows:    map[string]flowdef.Flow{"old": {ID: "old"}},
	}
	desired, err := Parse([]byte("settings: {big: 9007199254740993}\nscripts: {s: \"if (a <= b && c)\"}\nflows:\n"))
	if err != nil {
		t.Fatal(err)
	}
	p := LivePlan(desired, live)
	if strings.Join(p.Updated, ",") != "script/s,settings/big" || strings.Join(p.Removed, ",") != "flow/old" {
		t.Errorf("plan = %+v", p)
	}
	if !strings.Contains(p.Text(), `value: "if (a < b && c)" → "if (a <= b && c)"`) || !strings.Contains(p.Text(), "value: 9007199254740992 → 9007199254740993") {
		t.Errorf("text:\n%s", p.Text())
	}
	live.Scripts["s"] = "changed"
	if p.Fingerprint == LivePlan(desired, live).Fingerprint {
		t.Error("a live change must change the fingerprint")
	}
}

// TestFingerprintCoversManagedSections: leaving a section out and emptying it
// plan the same artifacts but differ in what apply removes.
func TestFingerprintCoversManagedSections(t *testing.T) {
	live := &Config{Flows: map[string]flowdef.Flow{}}
	leaveOut, _ := Parse([]byte("scripts: {}\n"))
	empty, _ := Parse([]byte("scripts: {}\nflows: {}\n"))
	if LivePlan(leaveOut, live).Fingerprint == LivePlan(empty, live).Fingerprint {
		t.Error("the fingerprint must tell a left-out section from an empty one")
	}
}

func TestSectionOf(t *testing.T) {
	for kind, want := range map[string]string{"flow": "flows", "library": "snippetLibraries", "configmap": "configmap", "settings": "settings", "nope": ""} {
		if got, ok := SectionOf(kind); got != want || ok != (want != "") {
			t.Errorf("%s = %q %v, want %q", kind, got, ok, want)
		}
	}
	for section, want := range map[string]string{"flows": "flow", "snippetLibraries": "library", "nope": ""} {
		if got, ok := KindOf(section); got != want || ok != (want != "") {
			t.Errorf("KindOf(%s) = %q %v, want %q", section, got, ok, want)
		}
	}
	// Every kind Artifacts produces has a section.
	c := &Config{Flows: map[string]flowdef.Flow{"f": {}}, Alerts: map[string]artifact.Alert{"a": {}}, Snippets: map[string]artifact.Snippet{"s": {}},
		SnippetLibraries: map[string]artifact.SnippetLibrary{"l": {}}, Scripts: map[string]string{"x": ""}, ConfigMap: map[string]string{"m": ""}, Settings: map[string]any{"k": 1}}
	for key := range c.Artifacts() {
		kind, _, _ := strings.Cut(key, "/")
		if _, ok := SectionOf(kind); !ok {
			t.Errorf("no section for %s", key)
		}
	}
}
