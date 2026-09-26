package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	invopop "github.com/invopop/jsonschema"
	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/weavster-dev/weavster/internal/flowdef"
)

// Schema returns the JSON Schema for the config root, generated from the Go
// types (arch §6; published to agent-docs/schemas/config.schema.json).
// Flows refer to the flow schema (flow.schema.json) by its $id.
func Schema() *invopop.Schema {
	r := new(invopop.Reflector)
	r.ExpandedStruct = true
	r.Mapper = func(t reflect.Type) *invopop.Schema {
		if t == reflect.TypeOf(flowdef.Flow{}) {
			return &invopop.Schema{Ref: flowdef.SchemaID}
		}
		return nil
	}
	return r.Reflect(&Config{})
}

// SchemaJSON returns the marshaled JSON Schema.
func SchemaJSON() ([]byte, error) {
	return Schema().MarshalJSON()
}

// Validate checks a config document against the generated JSON Schema,
// rejecting invalid configs on load (arch §6). Each flow is checked against
// flow.schema.json, so runtime fields such as status are rejected.
func Validate(data []byte) error {
	c, written, err := parse(data)
	if err != nil {
		return err
	}
	// Check each flow as written (unknown fields included) against the flow
	// schema, for messages that name the flow.
	keys := make([]string, 0, len(written))
	for k := range written {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var errs []string
	for _, k := range keys {
		fd, err := flowdef.ParseDoc(written[k])
		if err == nil {
			if _, ok := fd["id"]; !ok {
				fd["id"] = k
			}
			err = flowdef.ValidateDoc(fd)
		}
		if err != nil {
			errs = append(errs, fmt.Sprintf("flows.%s: %v", k, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("config: %s", strings.Join(errs, "; "))
	}
	js, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return validateJSON(js)
}

// configSchema is the compiled config schema (flows resolve to the
// embedded flow schema).
var configSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	schema, err := SchemaJSON()
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	if err := compiler.AddResource(flowdef.SchemaID, bytes.NewReader(flowdef.Schema)); err != nil {
		return nil, err
	}
	if err := compiler.AddResource("config.schema.json", bytes.NewReader(schema)); err != nil {
		return nil, err
	}
	return compiler.Compile("config.schema.json")
})

func validateJSON(js []byte) error {
	sch, err := configSchema()
	if err != nil {
		return err
	}
	var v any
	if err := json.Unmarshal(js, &v); err != nil {
		return err
	}
	return sch.Validate(v)
}
