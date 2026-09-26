package gateway

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// FlowSchema is the published flow-definition JSON Schema
// (agent-docs/schemas/flow.schema.json).
//
//go:embed flow.schema.json
var FlowSchema []byte

var flowSchema = mustCompileFlowSchema()

func mustCompileFlowSchema() *jsonschema.Schema {
	c := jsonschema.NewCompiler()
	c.Draft = jsonschema.Draft2020
	if err := c.AddResource("flow.schema.json", bytes.NewReader(FlowSchema)); err != nil {
		panic(err)
	}
	return c.MustCompile("flow.schema.json")
}

// validateFlowJSON checks one flow definition document against FlowSchema
// and returns a short, client-safe description of the first violations.
func validateFlowJSON(raw []byte) error {
	var probe struct {
		ID any `json:"id"`
	}
	if json.Unmarshal(raw, &probe) == nil {
		if id, ok := probe.ID.(string); ok && reservedFlowIDs[id] {
			return fmt.Errorf("flow id %q is reserved; ids must not be export, import, or redeploy-all", id)
		}
	}
	var doc any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return fmt.Errorf("flow is not valid JSON: %w", err)
	}
	err := flowSchema.Validate(doc)
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
