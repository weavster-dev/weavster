package compiler

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"
)

// Schema is the transform DSL's JSON Schema, the one source for the flow
// API, config-as-code, and Validate; published as
// agent-docs/schemas/transform.schema.json (go generate copies it there).
//
//go:generate cp transform.schema.json ../../agent-docs/schemas/transform.schema.json
//go:embed transform.schema.json
var Schema []byte

// SchemaID is the schema's $id, by which flow.schema.json refers to it.
const SchemaID = "https://raw.githubusercontent.com/weavster-dev/weavster/main/agent-docs/schemas/transform.schema.json"

var transformSchema = func() *jsonschema.Schema {
	c := jsonschema.NewCompiler()
	c.Draft = jsonschema.Draft2020
	if err := c.AddResource(SchemaID, bytes.NewReader(Schema)); err != nil {
		panic(err)
	}
	return c.MustCompile(SchemaID)
}()

// Validate checks a transform document (YAML or JSON) against Schema,
// rejecting invalid configs on load (arch §6). The document itself is
// checked, so unknown keys are refused rather than dropped.
func Validate(data []byte) error {
	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("transform is not valid YAML: %w", err)
	}
	js, err := json.Marshal(doc) // YAML maps decode as map[string]any
	if err != nil {
		return err
	}
	var v any
	if err := json.Unmarshal(js, &v); err != nil {
		return err
	}
	return transformSchema.Validate(v)
}
