package gateway

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestFlowSchemaPublished keeps the embedded schema and the published
// agent-docs copy identical (single source of truth).
func TestFlowSchemaPublished(t *testing.T) {
	published, err := os.ReadFile("../../agent-docs/schemas/flow.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(published, FlowSchema) {
		t.Error("agent-docs/schemas/flow.schema.json differs from internal/gateway/flow.schema.json; copy it over")
	}
}

func TestValidateFlowJSON(t *testing.T) {
	tests := []struct {
		name, doc, want string // want "" = valid
	}{
		{"minimal", `{"id":"a"}`, ""},
		{"full", `{"id":"a","name":"A","sourceType":"http","enabled":true,"dependsOn":["b"],
			"transform":{"kind":"Transform","name":"t","inputs":["message"],"steps":[{"map":{"from":"x","to":"y","type":"number"}},{"filter":{"when":"y","action":"accept"}}]},
			"destinations":[{"name":"d","type":"http","url":"https://x"},{"name":"f","type":"file","dir":"/tmp"}]}`, ""},
		{"null transform", `{"id":"a","transform":null}`, ""},
		{"unknown field", `{"id":"a","colour":"red"}`, "colour"},
		{"wrong type", `{"id":"a","enabled":"yes"}`, "/enabled"},
		{"bad destination type", `{"id":"a","destinations":[{"name":"d","type":"smtp"}]}`, "/destinations/0/type"},
		{"unknown step", `{"id":"a","transform":{"steps":[{"explode":{}}]}}`, "/transform"},
		{"two kinds in a step", `{"id":"a","transform":{"steps":[{"map":{"from":"a","to":"b"},"set":{"field":"c","expr":"d"}}]}}`, "/transform"},
		{"bad filter action", `{"id":"a","transform":{"steps":[{"filter":{"when":"x","action":"drop"}}]}}`, "/transform"},
		{"missing id", `{"name":"a"}`, "id"},
		{"bad id", `{"id":"a b"}`, "/id"},
		{"reserved id", `{"id":"import"}`, "is reserved"},
		{"duplicate dependency", `{"id":"a","dependsOn":["b","b"]}`, "/dependsOn"},
		{"not json", `{`, "not valid JSON"},
		{"many errors", `{"id":"a","enabled":"x","name":2,"sourceType":3,"dependsOn":4}`, "and 1 more"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFlowJSON([]byte(tt.doc))
			if tt.want == "" {
				if err != nil {
					t.Errorf("valid document rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want containing %q", err, tt.want)
			}
		})
	}
}
