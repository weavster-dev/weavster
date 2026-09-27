package config

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/artifact"
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
    name: Admit errors
    enabled: true
    trigger: {events: [message.errored], flows: [admit]}
    actions: [{type: email, to: [ops@example.com]}]
snippetLibraries:
  hl7: {description: HL7 helpers}
snippets:
  pid: {library: hl7, code: "get('PID.3')"}
scripts:
  deploy: log('deployed')
configmap:
  region: eu
settings:
  retention: {days: 30}
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
		Alerts: map[string]artifact.Alert{
			"on-error": {ID: "on-error", Name: "Errors", Enabled: true},
		},
		Snippets:         map[string]artifact.Snippet{"patient-name": {Name: "patient-name", Code: "PID.5"}},
		SnippetLibraries: map[string]artifact.SnippetLibrary{"hl7": {Name: "hl7"}},
		Scripts:          map[string]string{"transform": "return input"},
		ConfigMap:        map[string]string{"facility": "central"},
		Settings:         map[string]any{"retries": 3},
	}

	artifacts := c.Artifacts()
	if len(artifacts) != 7 {
		t.Fatalf("artifact count = %d, want 7", len(artifacts))
	}
	for key, want := range map[string]string{
		"snippet/patient-name": `{"name":"patient-name","code":"PID.5"}`,
		"library/hl7":          `{"name":"hl7"}`,
		"script/transform":     "return input",
		"configmap/facility":   "central",
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
	var alert artifact.Alert
	if err := json.Unmarshal(artifacts["alert/on-error"], &alert); err != nil {
		t.Fatalf("unmarshal alert artifact: %v", err)
	}
	if alert.Name != "Errors" || !alert.Enabled {
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
		{"old alert shape", `{"alerts": {"x": {"trigger": "t"}}}`, "config: parse"},
		{"unknown top-level field", "map: {a: b}\n", "field map not found"},
		{"unknown alert field", "alerts:\n  x: {name: X, recipients: [a]}\n", "field recipients not found"},
		{"alert id differs from key", "alerts:\n  x: {id: y}\n", `alerts.x: id "y" must match the key`},
		{"snippet name differs from key", "snippets:\n  x: {name: y}\n", `snippets.x: name "y" must match the key`},
		{"library name differs from key", "snippetLibraries:\n  x: {name: y}\n", `snippetLibraries.x: name "y" must match the key`},
		{"invalid alert", "alerts:\n  x: {name: X, trigger: {events: [message.sent]}, actions: [{type: webhook, url: \"https://h/x\"}]}\n", `alerts.x: alert x: unknown trigger event "message.sent"`},
		{"snippet library missing", "snippets:\n  s: {library: nope}\n", `snippets.s: library "nope" is not in snippetLibraries`},
		{"bad script name", "scripts:\n  \"a b\": x\n", "scripts.a b: name"},
		{"bad library name", "snippetLibraries:\n  \"a b\": {}\n", "snippetLibraries.a b: name"},
		{"bad snippet name", "snippets:\n  \"a b\": {}\n", "snippets.a b: name"},
		{"null setting", "settings:\n  s: null\n", "settings.s: value must not be null"},
		{"setting with a number key", "settings:\n  s: {1: x}\n", "settings.s: not JSON-compatible"},
		{"config map needs text", "configmap:\n  a: [1]\n", "config: parse"},
		{"empty document", "  \n", "the document is empty"},
		{"two YAML documents", "flows: {}\n---\nbogus: 1\n", "more than one YAML document"},
		{"unsupported version", "version: \"2\"\n", `version "2" is not supported`},
		{"bad alert key", "alerts:\n  \"a b\": {name: X, trigger: {events: [message.errored]}, actions: [{type: email, to: [a@b.co]}]}\n", `alerts.a b: name "a b" must be`},
		{"flow that is not JSON-compatible", "flows:\n  a: {name: {1: x}}\n", "not JSON-compatible"},
	}
	for _, tt := range tests {
		if err := Validate([]byte(tt.doc)); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: Validate = %v, want %q", tt.name, err, tt.want)
		}
	}
}

// updateSchemas rewrites the published schemas:
//
//	go test ./internal/config -run TestSchemasPublished -update
var updateSchemas = flag.Bool("update", false, "rewrite agent-docs/schemas from the Go types")

// TestSchemasPublished keeps agent-docs/schemas identical to the schemas
// generated from the Go types.
func TestSchemasPublished(t *testing.T) {
	schemas, err := PublishedSchemas()
	if err != nil {
		t.Fatal(err)
	}
	for name, generated := range schemas {
		path := "../../agent-docs/schemas/" + name
		if *updateSchemas {
			if err := os.WriteFile(path, append(generated, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		published, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (run with -update)", err)
		}
		if !bytes.Equal(bytes.TrimSpace(published), generated) {
			t.Errorf("%s is stale; run go test ./internal/config -run TestSchemasPublished -update", path)
		}
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
    name: Stale
    trigger: {events: [message.errored]}
    actions: [{type: email, to: [a@b.c]}]
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
alerts: {}
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
alerts:
`))
	live, _ := Parse([]byte(`
version: "1"
alerts:
  gone:
    name: Gone
    trigger: {events: [message.errored]}
    actions: [{type: email, to: [a@b.c]}]
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
	c, err := Parse([]byte("version: 1\nconfigmap: {port: 8080}\nflows:\n  123: {sourceType: http}\nsettings: {big: 9007199254740993}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Version != "1" || c.ConfigMap["port"] != "8080" || c.Flows["123"].ID != "123" {
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
