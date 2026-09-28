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

	"github.com/weavster-dev/weavster/internal/artifact"
	"github.com/weavster-dev/weavster/internal/flowdef"
)

// reflector generates schemas from the Go types: closed objects, with
// required fields taken from jsonschema tags; flows refer to the flow schema
// (flow.schema.json) by its $id.
func reflector() *invopop.Reflector {
	r := new(invopop.Reflector)
	r.ExpandedStruct = true
	r.RequiredFromJSONSchemaTags = true
	r.Mapper = func(t reflect.Type) *invopop.Schema {
		if t == reflect.TypeOf(flowdef.Flow{}) {
			return &invopop.Schema{Ref: flowdef.SchemaID}
		}
		return nil
	}
	return r
}

// Schema returns the JSON Schema for the config root, generated from the Go
// types (arch §6; published to agent-docs/schemas/config.schema.json).
func Schema() *invopop.Schema {
	return reflector().Reflect(&Config{})
}

// SchemaJSON returns the marshaled JSON Schema.
func SchemaJSON() ([]byte, error) {
	return Schema().MarshalJSON()
}

// PublishedSchemas returns every generated schema by its file name in
// agent-docs/schemas: the config root and one per artifact kind (the flow
// schema is maintained by hand in internal/flowdef).
func PublishedSchemas() (map[string][]byte, error) {
	out := map[string][]byte{}
	for name, v := range map[string]any{
		"alert.schema.json":           &artifact.Alert{},
		"snippet.schema.json":         &artifact.Snippet{},
		"snippet-library.schema.json": &artifact.SnippetLibrary{},
	} {
		sch := reflector().Reflect(v)
		sch.ID = invopop.ID(name)
		js, err := sch.MarshalJSON()
		if err != nil {
			return nil, err
		}
		out[name] = js
	}
	root, err := SchemaJSON()
	if err != nil {
		return nil, err
	}
	out["config.schema.json"] = root
	return out, nil
}

// Validate checks a config document against the generated JSON Schema,
// rejecting invalid configs on load (arch §6). Each flow is checked against
// flow.schema.json, so runtime fields such as status are rejected.
func Validate(data []byte) error {
	_, err := ParseValid(data)
	return err
}

// ParseValid parses and validates a config document in one pass and returns
// it when it is valid.
func ParseValid(data []byte) (*Config, error) {
	c, written, err := parse(data)
	if err != nil {
		return nil, err
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
	errs = append(errs, checkArtifacts(c)...)
	if len(errs) > 0 {
		return nil, fmt.Errorf("config: %s", strings.Join(errs, "; "))
	}
	js, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	if err := validateJSON(js); err != nil {
		return nil, err
	}
	return c, nil
}

// checkArtifacts applies the rules the API applies to alerts, snippets,
// libraries, and item names, plus the ones only a whole document can check:
// a snippet's library must be in the document, and settings must be JSON.
func checkArtifacts(c *Config) []string {
	var errs []string
	for _, k := range sortedKeys(c.Flows) {
		f := c.Flows[k]
		for _, err := range []error{flowdef.CheckSource(f.Source), flowdef.CheckInput(f)} {
			if err != nil {
				errs = append(errs, "flows."+k+": "+err.Error())
			}
		}
	}
	for _, k := range sortedKeys(c.Alerts) {
		if err := artifact.CheckName(k); err != nil {
			errs = append(errs, "alerts."+k+": "+err.Error())
		} else if err := artifact.CheckAlert(c.Alerts[k]); err != nil {
			errs = append(errs, "alerts."+k+": "+err.Error())
		}
	}
	for _, k := range sortedKeys(c.SnippetLibraries) {
		if err := artifact.CheckName(k); err != nil {
			errs = append(errs, "snippetLibraries."+k+": "+err.Error())
		}
	}
	for _, k := range sortedKeys(c.Snippets) {
		if err := artifact.CheckName(k); err != nil {
			errs = append(errs, "snippets."+k+": "+err.Error())
		}
		if lib := c.Snippets[k].Library; lib != "" {
			if _, ok := c.SnippetLibraries[lib]; !ok {
				errs = append(errs, fmt.Sprintf("snippets.%s: library %q is not in snippetLibraries", k, lib))
			}
		}
	}
	for section, names := range map[string][]string{"scripts": sortedKeys(c.Scripts), "configmap": sortedKeys(c.ConfigMap), "settings": sortedKeys(c.Settings)} {
		for _, k := range names {
			if err := artifact.CheckName(k); err != nil {
				errs = append(errs, section+"."+k+": "+err.Error())
			}
		}
	}
	for _, k := range sortedKeys(c.Settings) {
		if c.Settings[k] == nil {
			errs = append(errs, "settings."+k+": value must not be null")
		} else if _, err := json.Marshal(c.Settings[k]); err != nil {
			errs = append(errs, "settings."+k+": not JSON-compatible (use string keys)")
		}
	}
	sort.Strings(errs)
	return errs
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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
