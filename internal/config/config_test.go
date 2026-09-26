package config

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/flowdef"
)

const validYAML = `
version: "1"
flows:
  admit:
    name: Patient Admit
    sourceType: http
    enabled: true
    initialState: paused
    responseSelector: his
    transform:
      steps:
        - map: {from: PID.5.1, to: patient.lastName}
    destinations:
      - name: his
        type: http
        url: https://his.example.com/inbound
        responseTransform:
          steps:
            - map: {from: code, to: ack}
      - name: archive
        type: file
        dir: /var/lib/weavster/archive
        transform:
          steps:
            - filter: {when: "patient.lastName == ''", action: reject}
alerts:
  on-error:
    trigger: processing-error
    recipients: [ops@example.com]
    scope: flow:admit
    enabled: true
`

func TestParseYAMLAndJSON(t *testing.T) {
	c, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatalf("parse yaml: %v", err)
	}
	if c.Version != "1" {
		t.Errorf("version = %q", c.Version)
	}
	if _, ok := c.Flows["admit"]; !ok {
		t.Error("missing flow admit")
	}
	admit := c.Flows["admit"]
	if admit.ID != "admit" || admit.SourceType != "http" || admit.InitialState != "paused" || admit.ResponseSelector != "his" ||
		len(admit.Destinations) != 2 || !strings.Contains(string(admit.Transform), `"from":"PID.5.1"`) ||
		!strings.Contains(string(admit.Destinations[1].Transform), `"action":"reject"`) {
		t.Errorf("admit = %+v", admit)
	}

	// JSON is a YAML subset and must parse identically.
	j, err := c.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	again, err := Parse(j)
	if err != nil {
		t.Fatalf("parse marshaled yaml: %v", err)
	}
	a, _ := json.Marshal(c.Flows)
	b, _ := json.Marshal(again.Flows)
	if string(a) != string(b) {
		t.Errorf("flows changed on a YAML round trip:\n%s\n%s", a, b)
	}
}

func TestArtifactsFlattensAllConfigKinds(t *testing.T) {
	c := Config{
		Flows: map[string]flowdef.Flow{
			"admit": {ID: "admit", Name: "Admit", SourceType: "file"},
		},
		Alerts: map[string]Alert{
			"on-error": {Trigger: "processing-error", Recipients: []string{"ops@example.com"}, Enabled: true},
		},
		Snippets: map[string]string{"patient-name": "PID.5"},
		Scripts:  map[string]string{"transform": "return input"},
		Map:      map[string]string{"facility": "central"},
		Settings: map[string]any{"retries": 3},
	}

	artifacts := c.Artifacts()
	if len(artifacts) != 6 {
		t.Fatalf("artifact count = %d, want 6", len(artifacts))
	}
	for key, want := range map[string]string{
		"snippet/patient-name": "PID.5",
		"script/transform":     "return input",
		"map/facility":         "central",
		"settings/retries":     "3",
	} {
		if got := string(artifacts[key]); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}

	var flow flowdef.Flow
	if err := json.Unmarshal(artifacts["flow/admit"], &flow); err != nil {
		t.Fatalf("unmarshal flow artifact: %v", err)
	}
	if flow.Name != "Admit" || flow.SourceType != "file" {
		t.Errorf("flow artifact = %+v", flow)
	}
	var alert Alert
	if err := json.Unmarshal(artifacts["alert/on-error"], &alert); err != nil {
		t.Fatalf("unmarshal alert artifact: %v", err)
	}
	if alert.Trigger != "processing-error" || !alert.Enabled {
		t.Errorf("alert artifact = %+v", alert)
	}
}

func TestValidate(t *testing.T) {
	if err := Validate([]byte(validYAML)); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	tests := []struct{ name, doc, want string }{
		{"flows not an object", `{"flows": 5}`, "config: parse"},
		{"id differs from key", "flows:\n  a: {id: b}\n", `id "b" must match the key`},
		{"runtime status", "flows:\n  a: {status: started}\n", "'status' not allowed"},
		{"unknown field", "flows:\n  a: {colour: red}\n", "flows.a: flow does not match flow.schema.json"},
		{"bad destination type", "flows:\n  a: {destinations: [{name: d, type: smtp}]}\n", "/destinations/0/type"},
		{"reserved id", "flows:\n  import: {}\n", "is reserved"},
		{"bad alert", `{"alerts": {"x": {"trigger": "t"}}}`, "recipients"},
		{"flow that is not JSON-compatible", "flows:\n  a: {name: {1: x}}\n", "not JSON-compatible"},
	}
	for _, tt := range tests {
		if err := Validate([]byte(tt.doc)); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: Validate = %v, want %q", tt.name, err, tt.want)
		}
	}
}

// TestConfigSchemaPublished keeps the published config schema identical
// to the generated one.
func TestConfigSchemaPublished(t *testing.T) {
	published, err := os.ReadFile("../../agent-docs/schemas/config.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	generated, err := SchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(published), generated) {
		t.Error("agent-docs/schemas/config.schema.json is stale; regenerate it from config.SchemaJSON")
	}
	if !strings.Contains(string(generated), `"$ref":"`+flowdef.SchemaID+`"`) {
		t.Errorf("flows do not refer to flow.schema.json: %s", generated)
	}
}

func TestDiff(t *testing.T) {
	live, _ := Parse([]byte(`
version: "1"
flows:
  old:
    name: Old
    sourceType: file
alerts:
  stale:
    trigger: x
    recipients: [a@b.c]
`))
	desired, _ := Parse([]byte(`
version: "1"
flows:
  old:
    name: Old Renamed
    sourceType: file
  new:
    name: New
    sourceType: http
`))

	p := Diff(desired, live)
	if len(p.Added) != 1 || p.Added[0] != "flow/new" {
		t.Errorf("added = %v, want [flow/new]", p.Added)
	}
	if len(p.Updated) != 1 || p.Updated[0] != "flow/old" {
		t.Errorf("updated = %v, want [flow/old]", p.Updated)
	}
	if len(p.Removed) != 1 || p.Removed[0] != "alert/stale" {
		t.Errorf("removed = %v, want [alert/stale]", p.Removed)
	}
	if p.Empty() {
		t.Error("plan should not be empty")
	}
	if _, err := p.JSON(); err != nil {
		t.Errorf("plan json: %v", err)
	}
	if !strings.Contains(p.DiffText(), "+ flow/new") {
		t.Errorf("diff text missing added line: %q", p.DiffText())
	}
}

type captureAudit struct {
	calls int
}

func (c *captureAudit) Record(context.Context, string, map[string]any) error {
	c.calls++
	return nil
}

func TestApply(t *testing.T) {
	desired, _ := Parse([]byte(`
version: "1"
flows:
  a:
    name: A
    sourceType: file
`))
	live, _ := Parse([]byte(`
version: "1"
alerts:
  gone:
    trigger: x
    recipients: [a@b.c]
`))

	store := NewMemStore()
	for k, v := range live.Artifacts() {
		_ = store.Put(context.Background(), k, v)
	}

	p := Diff(desired, live)
	audit := &captureAudit{}
	if err := Apply(context.Background(), store, p, desired.Artifacts(), audit); err != nil {
		t.Fatalf("apply: %v", err)
	}

	arts, _ := store.List(context.Background())
	if _, ok := arts["flow/a"]; !ok {
		t.Error("flow/a not applied")
	}
	if _, ok := arts["alert/gone"]; ok {
		t.Error("alert/gone not removed")
	}
	if audit.calls != 1 {
		t.Errorf("audit calls = %d, want 1", audit.calls)
	}
}

func TestDetectDrift(t *testing.T) {
	source := NewMemSource(map[string][]byte{
		"flow/a": []byte("v1"),
		"flow/b": []byte("v2"),
	})
	store := NewMemStore()
	_ = store.Put(context.Background(), "flow/a", []byte("CHANGED"))
	_ = store.Put(context.Background(), "flow/rogue", []byte("x"))

	r, err := DetectDrift(source, store)
	if err != nil {
		t.Fatalf("drift: %v", err)
	}
	if len(r.Diverged) != 1 || r.Diverged[0] != "flow/a" {
		t.Errorf("diverged = %v, want [flow/a]", r.Diverged)
	}
	if len(r.Missing) != 1 || r.Missing[0] != "flow/b" {
		t.Errorf("missing = %v, want [flow/b]", r.Missing)
	}
	if len(r.OutOfBand) != 1 || r.OutOfBand[0] != "flow/rogue" {
		t.Errorf("outOfBand = %v, want [flow/rogue]", r.OutOfBand)
	}
}

// TestParseKeepsYAMLRules: only flows go through JSON; the rest of the
// document keeps YAML scalar rules and exact numbers.
func TestParseKeepsYAMLRules(t *testing.T) {
	c, err := Parse([]byte("version: 1\nmap: {port: 8080}\nflows:\n  123: {sourceType: http}\nsettings: {big: 9007199254740993}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Version != "1" || c.Map["port"] != "8080" || c.Flows["123"].ID != "123" {
		t.Errorf("config = %+v", c)
	}
	if got := string(c.Artifacts()["settings/big"]); got != "9007199254740993" {
		t.Errorf("settings/big = %s, want the exact integer", got)
	}
	out, err := c.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "big: 9007199254740993") || !strings.Contains(string(out), "sourceType: http") {
		t.Errorf("marshal = %s", out)
	}
}
