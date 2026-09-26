package flowdef

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"sort"
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
	if !bytes.Equal(published, Schema) {
		t.Error("agent-docs/schemas/flow.schema.json differs from internal/flowdef/flow.schema.json; run go generate ./internal/flowdef")
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
		{"runtime stoppedDestinations", `{"id":"a","stoppedDestinations":["d"]}`, "stoppedDestinations"},
		{"wrong type", `{"id":"a","enabled":"yes"}`, "/enabled"},
		{"bad destination type", `{"id":"a","destinations":[{"name":"d","type":"smtp"}]}`, "/destinations/0/type"},
		{"destination name with slash", `{"id":"a","destinations":[{"name":"ehr/primary","type":"file","dir":"/x"}]}`, "/destinations/0/name"},
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
			err := ValidateJSON([]byte(tt.doc))
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

func TestValidateFlowJSONNullsAndErrors(t *testing.T) {
	for _, doc := range []string{
		`{"id":"a","name":null,"sourceType":null,"enabled":null,"dependsOn":null,"destinations":null}`,
		`{"id":"a","transform":{"kind":"Transform","name":null,"inputs":null,"steps":null}}`,
		`{"id":"a","destinations":[{"name":"d","type":"file","dir":"/x","url":null}]}`,
	} {
		if err := ValidateJSON([]byte(doc)); err != nil {
			t.Errorf("%s rejected: %v", doc, err)
		}
	}
	err := ValidateJSON([]byte(`{"id":"a","transform":{"steps":[{"filter":{"when":"x","action":"drop"}}]}}`))
	if err == nil || strings.Contains(err.Error(), "expected null") {
		t.Errorf("transform error = %v; want only the real problem", err)
	}
	if err := ValidateJSON([]byte(`[1]`)); err == nil || !strings.Contains(err.Error(), "JSON object") {
		t.Errorf("non-object = %v", err)
	}
	if err := ValidateJSON([]byte(`{"id":"a"} garbage`)); err == nil || !strings.Contains(err.Error(), "trailing data") {
		t.Errorf("trailing data = %v", err)
	}
	for _, id := range []string{"export", "import", "redeploy-all"} {
		if !reservedIDs[id] {
			t.Errorf("%s not read from the schema as reserved", id)
		}
	}
}

// TestFlowSchemaMatchesGoTypes guards the hand-written schema against drift
// from Flow/Destination and the generated transform schema.
func TestFlowSchemaMatchesGoTypes(t *testing.T) {
	var schema struct {
		Properties map[string]any `json:"properties"`
		Defs       map[string]struct {
			Properties map[string]any `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(Schema, &schema); err != nil {
		t.Fatal(err)
	}
	jsonFields := func(v any, skip ...string) []string {
		var out []string
		rt := reflect.TypeOf(v)
		for i := 0; i < rt.NumField(); i++ {
			name, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
			if name != "" && !slices.Contains(skip, name) {
				out = append(out, name)
			}
		}
		sort.Strings(out)
		return out
	}
	keys := func(m map[string]any) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	if got, want := keys(schema.Properties), jsonFields(Flow{}, "status", "stoppedDestinations"); !slices.Equal(got, want) {
		t.Errorf("flow schema properties %v != flowdef.Flow fields %v (runtime status and stoppedDestinations excluded)", got, want)
	}
	if got, want := keys(schema.Defs["Destination"].Properties), jsonFields(Destination{}); !slices.Equal(got, want) {
		t.Errorf("Destination properties %v != Destination fields %v", got, want)
	}

	generated, err := os.ReadFile("../../agent-docs/schemas/transform.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var transform struct {
		Defs map[string]struct {
			Properties map[string]any `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(generated, &transform); err != nil {
		t.Fatal(err)
	}
	for name, def := range transform.Defs {
		if got, want := keys(schema.Defs[name].Properties), keys(def.Properties); !slices.Equal(got, want) {
			t.Errorf("$defs/%s properties %v != transform.schema.json %v", name, got, want)
		}
	}
}

func TestSchemaID(t *testing.T) {
	var s struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(Schema, &s); err != nil || s.ID != SchemaID {
		t.Errorf("$id = %q, %v; want %q", s.ID, err, SchemaID)
	}
}
