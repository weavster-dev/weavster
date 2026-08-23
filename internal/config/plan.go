package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// Plan is a machine-readable plan of config changes (added/updated/removed)
// produced before any mutation (arch §6, gap #6).
type Plan struct {
	Added   []string `json:"added"`
	Updated []string `json:"updated"`
	Removed []string `json:"removed"`
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
// (gap #6). Artifacts are compared by serialized content.
func Diff(desired, live *Config) Plan {
	d := desired.Artifacts()
	l := live.Artifacts()

	var p Plan
	for k, dv := range d {
		if lv, ok := l[k]; !ok {
			p.Added = append(p.Added, k)
		} else if !bytes.Equal(lv, dv) {
			p.Updated = append(p.Updated, k)
		}
	}
	for k := range l {
		if _, ok := d[k]; !ok {
			p.Removed = append(p.Removed, k)
		}
	}
	sort.Strings(p.Added)
	sort.Strings(p.Updated)
	sort.Strings(p.Removed)
	return p
}
