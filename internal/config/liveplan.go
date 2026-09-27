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

// KindOf returns the artifact key kind of a document section ("flows" holds
// "flow").
func KindOf(section string) (string, bool) {
	prefix, ok := sections[section]
	return strings.TrimSuffix(prefix, "/"), ok
}

// SectionOf returns the document section of an artifact key's kind (the
// part before "/", as in Artifacts: "flow" is in "flows").
func SectionOf(kind string) (string, bool) {
	for section, prefix := range sections {
		if prefix == kind+"/" {
			return section, true
		}
	}
	return "", false
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
	// The fingerprint covers the live configuration and the document, so
	// an apply can tell that both are what the plan was made from.
	p := Plan{Fingerprint: fingerprint(l, d, desired.managedSet())}
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

// managedSet lists the managed sections, so the fingerprint tells a
// document that leaves a section out from one that empties it.
func (c *Config) managedSet() map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for section := range sections {
		if c.manages(sections[section]) {
			out[section] = json.RawMessage("1")
		}
	}
	return out
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
// Numbers stay exact and <, >, & are not escaped.
func canonical(artifacts map[string][]byte) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(artifacts))
	for k, v := range artifacts {
		if strings.HasPrefix(k, "script/") || strings.HasPrefix(k, "configmap/") {
			out[k] = mustRaw(string(v))
			continue
		}
		x, err := decodeExact(v)
		if err != nil {
			out[k] = v
			continue
		}
		out[k] = mustRaw(x)
	}
	return out
}

// decodeExact decodes JSON keeping numbers as written (json.Number).
func decodeExact(v []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(v))
	dec.UseNumber()
	var x any
	err := dec.Decode(&x)
	return x, err
}

// fieldChanges lists the values that differ between two JSON documents.
func fieldChanges(before, after json.RawMessage) []FieldChange {
	b, _ := decodeExact(before) // canonical JSON always decodes
	a, _ := decodeExact(after)
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

// mustRaw encodes decoded JSON (which always encodes) compactly, without
// HTML escaping, so code in scripts and snippets stays readable.
func mustRaw(v any) json.RawMessage {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}

// fingerprint hashes sets of artifacts in order: it changes whenever any
// artifact of any set does, so a plan can be recognized as stale later.
func fingerprint(sets ...map[string]json.RawMessage) string {
	h := sha256.New()
	for _, arts := range sets {
		for _, k := range sortedKeys(arts) {
			h.Write([]byte(k))
			h.Write([]byte{0})
			h.Write(arts[k])
			h.Write([]byte{0})
		}
		h.Write([]byte{1}) // ends a set
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
