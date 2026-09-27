package config

import (
	"encoding/json"
	"fmt"
)

// Plan is a machine-readable plan of config changes (added/updated/removed)
// produced before any mutation (arch §6, gap #6).
type Plan struct {
	Added   []string `json:"added"`
	Updated []string `json:"updated"`
	Removed []string `json:"removed"`
	// Unchanged counts artifacts that already match.
	Unchanged int `json:"unchanged,omitempty"`
	// Fingerprint identifies the live configuration and the document the
	// plan compares.
	Fingerprint string `json:"fingerprint,omitempty"`
	// Changes describes each change with its content (LivePlan).
	Changes []Change `json:"changes,omitempty"`
}

// Empty reports whether the plan contains no changes.
func (p Plan) Empty() bool {
	return len(p.Added) == 0 && len(p.Updated) == 0 && len(p.Removed) == 0
}

// JSON returns the machine-readable JSON plan.
func (p Plan) JSON() ([]byte, error) { return json.MarshalIndent(p, "", "  ") }

// DiffText returns a --diff-style human-readable plan.
func (p Plan) DiffText() string {
	var out string
	for _, k := range p.Added {
		out += fmt.Sprintf("+ %s\n", k)
	}
	for _, k := range p.Updated {
		out += fmt.Sprintf("~ %s\n", k)
	}
	for _, k := range p.Removed {
		out += fmt.Sprintf("- %s\n", k)
	}
	return out
}

// Diff computes the plan of changes between the desired and live configs
// (gap #6): LivePlan, so apply and drift plan exactly what config diff shows.
func Diff(desired, live *Config) Plan {
	return LivePlan(desired, live)
}
