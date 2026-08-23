package config

import (
	"bytes"
	"encoding/json"

	invopop "github.com/invopop/jsonschema"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

// Schema returns the JSON Schema for the config root, generated from the Go
// types (arch §6; published to agent-docs/schemas/ in P7).
func Schema() *invopop.Schema {
	r := new(invopop.Reflector)
	r.ExpandedStruct = true
	return r.Reflect(&Config{})
}

// SchemaJSON returns the marshaled JSON Schema.
func SchemaJSON() ([]byte, error) {
	return Schema().MarshalJSON()
}

// Validate checks a config document against the generated JSON Schema,
// rejecting invalid configs on load (arch §6).
func Validate(data []byte) error {
	c, err := Parse(data)
	if err != nil {
		return err
	}
	js, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return validateJSON(js)
}

func validateJSON(js []byte) error {
	schema, err := SchemaJSON()
	if err != nil {
		return err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("config.schema.json", bytes.NewReader(schema)); err != nil {
		return err
	}
	sch, err := compiler.Compile("config.schema.json")
	if err != nil {
		return err
	}
	var v any
	if err := json.Unmarshal(js, &v); err != nil {
		return err
	}
	return sch.Validate(v)
}
