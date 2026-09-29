package topology

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// Schema is the topology graph JSON Schema, published as
// agent-docs/schemas/topology.schema.json (go generate copies it there).
//
//go:generate cp topology.schema.json ../../agent-docs/schemas/topology.schema.json
//go:embed topology.schema.json
var Schema []byte

const schemaID = "https://raw.githubusercontent.com/weavster-dev/weavster/main/agent-docs/schemas/topology.schema.json"

// graphSchema compiles the schema the first time Validate needs it.
var graphSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	c := jsonschema.NewCompiler()
	c.AssertFormat = true
	if err := c.AddResource(schemaID, bytes.NewReader(Schema)); err != nil {
		return nil, err
	}
	return c.Compile(schemaID)
})

// Validate checks a graph payload (JSON) against the schema.
func Validate(payload []byte) error {
	schema, err := graphSchema()
	if err != nil {
		return err
	}
	var v any
	if err := json.Unmarshal(payload, &v); err != nil {
		return err
	}
	return schema.Validate(v)
}
