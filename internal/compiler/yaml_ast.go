package compiler

// Transform is the declarative transform document (arch §4.1 Path A).
type Transform struct {
	Kind   string   `json:"kind" yaml:"kind"`
	Name   string   `json:"name" yaml:"name"`
	Inputs []string `json:"inputs" yaml:"inputs"`
	Steps  []Step   `json:"steps" yaml:"steps"`
}

// Step is one declarative step. Exactly one of the step pointers is set.
type Step struct {
	Map            *MapStep            `json:"map,omitempty" yaml:"map,omitempty"`
	Set            *SetStep            `json:"set,omitempty" yaml:"set,omitempty"`
	Filter         *FilterStep         `json:"filter,omitempty" yaml:"filter,omitempty"`
	Build          *BuildStep          `json:"build,omitempty" yaml:"build,omitempty"`
	DestinationSet *DestinationSetStep `json:"destinationSet,omitempty" yaml:"destinationSet,omitempty"`
}

// MapStep copies a source field to a target field.
type MapStep struct {
	From string `json:"from" yaml:"from"`
	To   string `json:"to" yaml:"to"`
	Type string `json:"type,omitempty" yaml:"type,omitempty"`
}

// SetStep assigns a computed expression to a field.
type SetStep struct {
	Field string `json:"field" yaml:"field"`
	Expr  string `json:"expr" yaml:"expr"`
}

// FilterStep rejects or accepts a message based on an expression.
type FilterStep struct {
	When   string `json:"when" yaml:"when"`
	Action string `json:"action" yaml:"action"` // "reject" | "accept"
}

// BuildStep assembles output from a template (XSLT-style/assembly).
type BuildStep struct {
	Template string `json:"template" yaml:"template"`
	Format   string `json:"format,omitempty" yaml:"format,omitempty"`
}

// DestinationSetStep computes the allowed destination set (spec §2.2.5).
type DestinationSetStep struct {
	Include []string `json:"include,omitempty" yaml:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty" yaml:"exclude,omitempty"`
}
