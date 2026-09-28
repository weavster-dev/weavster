package compiler

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

const exampleYAML = `
kind: Transform
name: normalize-patient-name
inputs: [message]
steps:
  - map: { from: "PID.5.1", to: "patient.lastName", type: string }
  - map: { from: "PID.5.2", to: "patient.firstName", type: string }
  - set: { field: "patient.fullName", expr: "{{patient.lastName}}, {{patient.firstName}}" }
  - filter: { when: "patient.lastName == ''", action: reject }
  - destinationSet: { exclude: [archive] }
`

func TestParse(t *testing.T) {
	tr, err := Parse([]byte(exampleYAML))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if tr.Name != "normalize-patient-name" || tr.Kind != "Transform" {
		t.Errorf("transform = %+v", tr)
	}
	if len(tr.Steps) != 5 {
		t.Fatalf("steps = %d, want 5", len(tr.Steps))
	}
	if tr.Steps[0].Map == nil || tr.Steps[0].Map.From != "PID.5.1" {
		t.Errorf("step0 = %+v", tr.Steps[0])
	}
	if tr.Steps[3].Filter == nil || tr.Steps[3].Filter.Action != "reject" {
		t.Errorf("step3 = %+v", tr.Steps[3])
	}
	if tr.Steps[4].DestinationSet == nil || len(tr.Steps[4].DestinationSet.Exclude) != 1 {
		t.Errorf("step4 = %+v", tr.Steps[4])
	}
}

func TestGenerate(t *testing.T) {
	tr, _ := Parse([]byte(exampleYAML))
	src, err := Generate(tr)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	text := string(src)
	if !strings.Contains(text, "PID.5.1") || !strings.Contains(text, "patient.fullName") {
		t.Errorf("generated source missing mapped fields:\n%s", text)
	}
	if !strings.Contains(text, "//go:export transform") {
		t.Errorf("generated source missing export directive:\n%s", text)
	}

	// Deterministic output.
	src2, _ := Generate(tr)
	if string(src) != string(src2) {
		t.Error("generate must be deterministic")
	}
}

func TestCompileAndValidate(t *testing.T) {
	tr, res, err := Compile([]byte(runnableYAML))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if tr == nil || res == nil || res.Digest == "" || len(res.GoSource) == 0 {
		t.Error("compile returned empty result")
	}

	if err := Validate([]byte(`steps: "not-an-array"`)); err == nil {
		t.Error("expected invalid transform to be rejected")
	}
}

// runnableYAML uses only the steps the interpreter runs.
const runnableYAML = `
kind: Transform
name: normalize-patient-name
inputs: [message]
steps:
  - map: { from: "PID.5.1", to: "patient.lastName", type: string }
  - set: { field: "patient.fullName", expr: "{{patient.lastName}}" }
  - filter: { when: "patient.lastName == ''", action: reject }
`

func TestValidateAcceptsValidTransform(t *testing.T) {
	for _, doc := range []string{runnableYAML, "steps: []", "name: t", "{}", `{"name":"t","steps":[{"map":{"from":"a","to":"b"}}]}`} {
		if err := Validate([]byte(doc)); err != nil {
			t.Errorf("validate %q: %v", doc, err)
		}
	}
}

// TestValidateRefuses: the schema describes exactly what runs.
func TestValidateRefuses(t *testing.T) {
	for name, doc := range map[string]string{
		"build":            "steps:\n  - build: { template: x }",
		"destinationSet":   "steps:\n  - destinationSet: { exclude: [a] }",
		"two kinds":        "steps:\n  - map: { from: a, to: b }\n    set: { field: c, expr: d }",
		"empty step":       "steps:\n  - {}",
		"unknown key":      "name: t\ncolor: red",
		"bad action":       "steps:\n  - filter: { when: a, action: drop }",
		"bad type":         "steps:\n  - map: { from: a, to: b, type: date }",
		"missing to":       "steps:\n  - map: { from: a }",
		"empty path":       "steps:\n  - map: { from: '', to: b }",
		"not YAML":         "steps: [",
		"unknown step key": "steps:\n  - map: { from: a, to: b, via: c }",
	} {
		if err := Validate([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// TestSchemaPublishedAndMatchesTypes: agent-docs holds the embedded schema,
// and its properties are the Go types' JSON fields (build and
// destinationSet steps are not run yet, so the schema leaves them out).
func TestSchemaPublishedAndMatchesTypes(t *testing.T) {
	published, err := os.ReadFile("../../agent-docs/schemas/transform.schema.json")
	if err != nil || !bytes.Equal(published, Schema) {
		t.Errorf("agent-docs/schemas/transform.schema.json differs from internal/compiler/transform.schema.json (%v); run go generate ./internal/compiler", err)
	}
	var s struct {
		ID   string `json:"$id"`
		Defs map[string]struct {
			Properties map[string]any `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(Schema, &s); err != nil || s.ID != SchemaID {
		t.Fatalf("$id = %q, %v", s.ID, err)
	}
	fields := func(v any, skip ...string) []string {
		var out []string
		rt := reflect.TypeOf(v)
		for i := 0; i < rt.NumField(); i++ {
			if name, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ","); !slices.Contains(skip, name) {
				out = append(out, name)
			}
		}
		sort.Strings(out)
		return out
	}
	for name, v := range map[string]any{"Transform": Transform{}, "MapStep": MapStep{}, "SetStep": SetStep{}, "FilterStep": FilterStep{}, "Step": Step{}} {
		var got []string
		for k := range s.Defs[name].Properties {
			got = append(got, k)
		}
		sort.Strings(got)
		if want := fields(v, "build", "destinationSet"); !slices.Equal(got, want) {
			t.Errorf("$defs/%s properties %v, Go fields %v", name, got, want)
		}
	}
}

func TestBuildRequiresTinyGo(t *testing.T) {
	if _, err := exec.LookPath("tinygo"); err == nil {
		t.Skip("tinygo present; skipping absence test")
	}
	if _, err := Build(context.Background(), []byte("package main"), t.TempDir()); err == nil {
		t.Error("expected error when tinygo is absent")
	}
}

func TestCompileErrorPaths(t *testing.T) {
	// Invalid YAML must fail at parse.
	if _, _, err := Compile([]byte(":\tinvalid")); err == nil {
		t.Error("expected parse error for invalid YAML")
	}
}

func TestGenerateAllStepTypes(t *testing.T) {
	src, err := Generate(&Transform{
		Kind: "Transform",
		Name: "all-steps",
		Steps: []Step{
			{Map: &MapStep{From: "a.b", To: "c.d"}},
			{Set: &SetStep{Field: "x", Expr: "val"}},
			{Filter: &FilterStep{When: "x == ''", Action: "reject"}},
			{Build: &BuildStep{Template: "hello {{name}}"}},
			{DestinationSet: &DestinationSetStep{Exclude: []string{"a", "b"}}},
			{}, // empty step
		},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	text := string(src)
	for _, want := range []string{`"map"`, `"set"`, `"filter"`, `"build"`, `"destinationSet"`, `"empty"`} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %s in generated source", want)
		}
	}
}
