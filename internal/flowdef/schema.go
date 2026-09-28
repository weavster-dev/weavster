package flowdef

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/weavster-dev/weavster/internal/compiler"
)

// Schema is the flow-definition JSON Schema, published as
// agent-docs/schemas/flow.schema.json (go generate copies it there).
//
//go:generate cp flow.schema.json ../../agent-docs/schemas/flow.schema.json
//go:embed flow.schema.json
var Schema []byte

// SchemaID is the schema's $id, by which other schemas refer to it.
const SchemaID = "https://raw.githubusercontent.com/weavster-dev/weavster/main/agent-docs/schemas/flow.schema.json"

var flowSchema = mustCompileFlowSchema()

// reservedIDs are the ids the schema forbids (properties.id.not.enum):
// path segments used by /flows/<name> routes. Read from the schema so it
// stays the single source.
var reservedIDs = mustReservedIDs()

// Reserved reports whether id names an API route and cannot be a flow id.
func Reserved(id string) bool { return reservedIDs[id] }

func mustReservedIDs() map[string]bool {
	var s struct {
		Properties struct {
			ID struct {
				Not struct {
					Enum []string `json:"enum"`
				} `json:"not"`
			} `json:"id"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(Schema, &s); err != nil || len(s.Properties.ID.Not.Enum) == 0 {
		panic("flow.schema.json: properties.id.not.enum (reserved ids) is missing")
	}
	out := make(map[string]bool, len(s.Properties.ID.Not.Enum))
	for _, id := range s.Properties.ID.Not.Enum {
		out[id] = true
	}
	return out
}

func mustCompileFlowSchema() *jsonschema.Schema {
	c := jsonschema.NewCompiler()
	c.Draft = jsonschema.Draft2020
	if err := c.AddResource(SchemaID, bytes.NewReader(Schema)); err != nil {
		panic(err)
	}
	// Transforms are described by the DSL's own schema.
	if err := c.AddResource(compiler.SchemaID, bytes.NewReader(compiler.Schema)); err != nil {
		panic(err)
	}
	return c.MustCompile(SchemaID)
}

// ParseDoc decodes a flow definition for validation, keeping numbers
// exact.
func ParseDoc(raw []byte) (map[string]any, error) {
	var doc any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("flow is not valid JSON: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("flow is not valid JSON: trailing data after the document")
	}
	obj, ok := doc.(map[string]any)
	if !ok {
		return nil, errors.New("the flow must be a JSON object")
	}
	return obj, nil
}

// ValidateDoc checks a parsed flow definition against Schema and returns a
// short, client-safe description of the first violations.
func ValidateDoc(doc map[string]any) error {
	if id, ok := doc["id"].(string); ok && reservedIDs[id] {
		return fmt.Errorf("flow id %q is reserved (it names an API route)", id)
	}
	err := flowSchema.Validate(any(doc))
	if err == nil {
		return nil
	}
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return err
	}
	var msgs []string
	for _, leaf := range leaves(ve) {
		loc := leaf.InstanceLocation
		if loc == "" {
			loc = "/"
		}
		msgs = append(msgs, loc+": "+leaf.Message)
	}
	sort.Strings(msgs)
	if len(msgs) > 3 {
		msgs = append(msgs[:3], fmt.Sprintf("and %d more", len(msgs)-3))
	}
	return fmt.Errorf("flow does not match flow.schema.json: %s", strings.Join(msgs, "; "))
}

// ValidateJSON parses and validates one flow definition document.
func ValidateJSON(raw []byte) error {
	doc, err := ParseDoc(raw)
	if err != nil {
		return err
	}
	return ValidateDoc(doc)
}

// leaves returns the most specific validation errors.
func leaves(ve *jsonschema.ValidationError) []*jsonschema.ValidationError {
	if len(ve.Causes) == 0 {
		return []*jsonschema.ValidationError{ve}
	}
	var out []*jsonschema.ValidationError
	for _, c := range ve.Causes {
		out = append(out, leaves(c)...)
	}
	return out
}
