package topology

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// TestSchemaPublished: the published schema is this one.
func TestSchemaPublished(t *testing.T) {
	published, err := os.ReadFile("../../agent-docs/schemas/topology.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(published, Schema) {
		t.Error("agent-docs/schemas/topology.schema.json differs from internal/topology/topology.schema.json; run go generate ./internal/topology")
	}
}

// TestSchemaValidates: built graphs validate; invalid payloads do not.
func TestSchemaValidates(t *testing.T) {
	act := &Activity{Received: 1, LastMessageAt: "2026-09-29T10:00:00Z"}
	for name, g := range map[string]Graph{
		"empty":    NewGraph(),
		"overview": Overview([]FlowSummary{{ID: "a", Name: "A", Status: "started", Activity: act, Routes: []string{"b"}, Deps: []string{"b"}}, {ID: "b", Status: "errored"}}),
		"flow": FlowInternal(FlowDetail{ID: "a", Status: "started", Source: &Part{ID: "http", Label: "http", Status: "started", Activity: act, EdgeStatus: "active"},
			Transform: &Part{ID: "dsl:t", Label: "t", Meta: map[string]string{"steps": "2"}}, Destinations: []Part{{ID: "d", Label: "d", Status: "stopped", EdgeStatus: "idle"}},
			Routes: []Route{{Destination: "d", Flow: "b"}}}),
	} {
		b, _ := json.Marshal(g)
		if err := Validate(b); err != nil {
			t.Errorf("%s: %v\n%s", name, err, b)
		}
	}
	for _, bad := range []string{
		`{"schemaVersion":"1","generatedAt":"2026-09-29T10:00:00Z","nodes":null,"edges":[]}`,
		`{"schemaVersion":"1","generatedAt":"2026-09-29T10:00:00Z","nodes":[{"id":"flow:a","kind":"flow","label":"a","x":1}],"edges":[]}`,
		`{"schemaVersion":"1","generatedAt":"2026-09-29T10:00:00Z","nodes":[{"id":"flow:a","kind":"flow","label":"a","activity":{"sent":1}}],"edges":[]}`,
		`{"schemaVersion":"1","generatedAt":"soon","nodes":[],"edges":[]}`,
		`not json`,
	} {
		if Validate([]byte(bad)) == nil {
			t.Errorf("valid: %s", bad)
		}
	}
}
