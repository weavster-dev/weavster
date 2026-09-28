package compiler

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
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
	doc, err := stringKeys(doc)
	if err != nil {
		return err
	}
	js, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	var v any
	if err := json.Unmarshal(js, &v); err != nil {
		return err
	}
	return transformSchema.Validate(v)
}

// stringKeys returns v with every YAML mapping as map[string]any; a mapping
// key that is not a string (1: x, true: x) is refused, since JSON and the
// schema have only string keys.
func stringKeys(v any) (any, error) {
	switch t := v.(type) {
	case map[string]any:
		for k, e := range t {
			n, err := stringKeys(e)
			if err != nil {
				return nil, err
			}
			t[k] = n
		}
		return t, nil
	case map[any]any:
		return nil, errors.New("transform: mapping keys must be strings")
	case []any:
		for i, e := range t {
			n, err := stringKeys(e)
			if err != nil {
				return nil, err
			}
			t[i] = n
		}
	}
	return v, nil
}
