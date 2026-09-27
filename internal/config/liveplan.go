package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// sections maps a document section to the prefix of its artifact keys.
var sections = map[string]string{
	"flows": "flow/", "alerts": "alert/", "snippets": "snippet/", "snippetLibraries": "library/",
	"scripts": "script/", "configmap": "configmap/", "settings": "settings/",
}

// Change is one planned change to an artifact.
type Change struct {
	Key    string          `json:"key"`
	Action string          `json:"action"` // add, update, or remove
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
	// Fields lists what an update changes.
	Fields []FieldChange `json:"fields,omitempty"`
}

// FieldChange is one changed value inside an updated artifact; Path uses
// dots and [index] ("destinations[0].url"). A missing side is omitted.
type FieldChange struct {
	Path   string          `json:"path"`
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
}

// LivePlan compares desired with the live configuration: every artifact
// desired has that live lacks or holds differently, and every live artifact
// of a section desired manages that desired lacks. Sections the document
// leaves out are not managed (#107 D-47). Artifacts compare as canonical
// JSON, so formatting and key order never show as changes.
func LivePlan(desired, live *Config) Plan {
	d, l := canonical(desired.Artifacts()), canonical(live.Artifacts())
	var p Plan
	for _, k := range sortedKeys(d) {
		switch lv, ok := l[k]; {
		case !ok:
			p.Added = append(p.Added, k)
			p.Changes = append(p.Changes, Change{Key: k, Action: "add", After: d[k]})
		case !bytes.Equal(lv, d[k]):
			p.Updated = append(p.Updated, k)
			p.Changes = append(p.Changes, Change{Key: k, Action: "update", Before: lv, After: d[k], Fields: fieldChanges(lv, d[k])})
		default:
			p.Unchanged++
		}
	}
	for _, k := range sortedKeys(l) {
		if _, ok := d[k]; !ok && desired.manages(k) {
			p.Removed = append(p.Removed, k)
			p.Changes = append(p.Changes, Change{Key: k, Action: "remove", Before: l[k]})
		}
	}
	return p
}

// manages reports whether the document manages the section key belongs to.
func (c *Config) manages(key string) bool {
	if c.Managed == nil {
		return true // built in code, not parsed: everything is managed
	}
	for section, prefix := range sections {
		if strings.HasPrefix(key, prefix) {
			return c.Managed[section]
		}
	}
	return false
}

// canonical turns artifact contents into compact JSON with sorted keys;
// script and config-map values, stored as text, become JSON strings.
func canonical(artifacts map[string][]byte) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(artifacts))
	for k, v := range artifacts {
		if strings.HasPrefix(k, "script/") || strings.HasPrefix(k, "configmap/") {
			out[k], _ = json.Marshal(string(v)) // a string always encodes
			continue
		}
		var x any
		if json.Unmarshal(v, &x) != nil {
			out[k] = v
			continue
		}
		out[k], _ = json.Marshal(x) // decoded JSON always encodes
	}
	return out
}

// fieldChanges lists the values that differ between two JSON documents.
func fieldChanges(before, after json.RawMessage) []FieldChange {
	var b, a any
	_ = json.Unmarshal(before, &b)
	_ = json.Unmarshal(after, &a)
	var out []FieldChange
	walkDiff("", b, a, &out)
	for i := range out {
		if out[i].Path == "" {
			out[i].Path = "value" // a script or config-map text, or a setting
		}
	}
	return out
}

func walkDiff(path string, b, a any, out *[]FieldChange) {
	bm, bok := b.(map[string]any)
	am, aok := a.(map[string]any)
	if bok && aok {
		keys := map[string]bool{}
		for k := range bm {
			keys[k] = true
		}
		for k := range am {
			keys[k] = true
		}
		for _, k := range sortedKeys(keys) {
			bv, bin := bm[k]
			av, ain := am[k]
			p := join(path, k)
			switch {
			case !bin:
				*out = append(*out, FieldChange{Path: p, After: mustRaw(av)})
			case !ain:
				*out = append(*out, FieldChange{Path: p, Before: mustRaw(bv)})
			default:
				walkDiff(p, bv, av, out)
			}
		}
		return
	}
	bl, bok := b.([]any)
	al, aok := a.([]any)
	if bok && aok && len(bl) == len(al) {
		for i := range bl {
			walkDiff(fmt.Sprintf("%s[%d]", path, i), bl[i], al[i], out)
		}
		return
	}
	if !reflect.DeepEqual(b, a) {
		*out = append(*out, FieldChange{Path: path, Before: mustRaw(b), After: mustRaw(a)})
	}
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func mustRaw(v any) json.RawMessage {
	b, _ := json.Marshal(v) // decoded JSON always encodes
	return b
}

// Fingerprint identifies the live configuration: it changes whenever any
// live artifact does, so a plan made against one state can be recognized
// as stale later.
func Fingerprint(live *Config) string {
	arts := canonical(live.Artifacts())
	h := sha256.New()
	for _, k := range sortedKeys(arts) {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write(arts[k])
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Text renders the plan for people: "+ key" to add, "- key" to remove, and
// "~ key" to update with one indented line per changed field.
func (p Plan) Text() string {
	if len(p.Changes) == 0 {
		return "no changes\n"
	}
	var b strings.Builder
	for _, c := range p.Changes {
		mark := map[string]string{"add": "+", "update": "~", "remove": "-"}[c.Action]
		fmt.Fprintf(&b, "%s %s\n", mark, c.Key)
		for _, f := range c.Fields {
			switch {
			case f.Before == nil:
				fmt.Fprintf(&b, "    %s: (none) → %s\n", f.Path, f.After)
			case f.After == nil:
				fmt.Fprintf(&b, "    %s: %s → (none)\n", f.Path, f.Before)
			default:
				fmt.Fprintf(&b, "    %s: %s → %s\n", f.Path, f.Before, f.After)
			}
		}
	}
	fmt.Fprintf(&b, "%d to add, %d to change, %d to remove, %d unchanged\n", len(p.Added), len(p.Updated), len(p.Removed), p.Unchanged)
	return b.String()
}
