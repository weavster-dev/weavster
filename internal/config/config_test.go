package config

import (
	"context"
	"strings"
	"testing"
)

const validYAML = `
version: "1"
flows:
  admit:
    name: Patient Admit
    source:
      type: file
      config:
        path: /incoming/patients
    destinations:
      - name: his-mllp
        type: tcp
        config:
          mode: mllp
    transforms:
      - kind: map
        spec:
          from: PID.5.1
          to: patient.lastName
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
	if c.Flows["admit"].Source.Type != "file" {
		t.Errorf("source type = %q", c.Flows["admit"].Source.Type)
	}

	// JSON is a YAML subset and must parse identically.
	j, err := c.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(j); err != nil {
		t.Fatalf("parse marshaled yaml: %v", err)
	}
}

func TestValidate(t *testing.T) {
	if err := Validate([]byte(validYAML)); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	// flows must be an object; a scalar is invalid.
	if err := Validate([]byte(`{"flows": 5}`)); err == nil {
		t.Error("expected invalid config to be rejected")
	}
}

func TestDiff(t *testing.T) {
	live, _ := Parse([]byte(`
version: "1"
flows:
  old:
    name: Old
    source: {type: file}
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
    source: {type: file}
  new:
    name: New
    source: {type: http}
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
    source: {type: file}
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
