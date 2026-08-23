package compiler

import (
	"context"
	"os/exec"
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
	tr, res, err := Compile([]byte(exampleYAML))
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

func TestBuildRequiresTinyGo(t *testing.T) {
	if _, err := exec.LookPath("tinygo"); err == nil {
		t.Skip("tinygo present; skipping absence test")
	}
	if _, err := Build(context.Background(), []byte("package main"), t.TempDir()); err == nil {
		t.Error("expected error when tinygo is absent")
	}
}
