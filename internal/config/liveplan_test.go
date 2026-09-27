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
	first, second := Fingerprint(live), Fingerprint(live)
	if first == Fingerprint(desired) || first != second {
		t.Error("fingerprint must follow the content")
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
