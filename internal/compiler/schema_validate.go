package compiler

import (
	"bytes"
	"encoding/json"

	invopop "github.com/invopop/jsonschema"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

// SchemaJSON returns the JSON Schema for the transform DSL (published to
// agent-docs/schemas/ in P7).
func SchemaJSON() ([]byte, error) {
	return new(invopop.Reflector).Reflect(&Transform{}).MarshalJSON()
}

// Validate checks a transform document against its JSON Schema, rejecting
// invalid configs on load (arch §6).
func Validate(data []byte) error {
	t, err := Parse(data)
	if err != nil {
		return err
	}
	js, err := json.Marshal(t)
	if err != nil {
		return err
	}

	schema, err := SchemaJSON()
	if err != nil {
		return err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("transform.schema.json", bytes.NewReader(schema)); err != nil {
		return err
	}
	sch, err := compiler.Compile("transform.schema.json")
	if err != nil {
		return err
	}
	var v any
	if err := json.Unmarshal(js, &v); err != nil {
		return err
	}
	return sch.Validate(v)
}
