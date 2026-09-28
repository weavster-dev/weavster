package codecs

import (
	"errors"
	"maps"
	"regexp"
	"strconv"
	"strings"
)

// ErrNotHL7 reports input that is not an HL7 v2 message (no MSH segment
// first).
var ErrNotHL7 = errors.New("not an HL7 v2 message (no MSH segment first)")

// hl7Version matches the HL7 v2 versions read (MSH-12, surrounding spaces
// ignored): 2.1 to 2.9, with an optional minor release (2.3.1, 2.5.1,
// 2.3.0).
var hl7Version = regexp.MustCompile(`^2\.[1-9](\.[0-9]+)?$`)

// HL7JSON parses an HL7 v2 message into the JSON view the DSL reads (#107
// D-61): each segment name holds its first occurrence, and "segments" holds
// every segment in order (each with its "name"). A segment maps HL7 field
// numbers ("1", "2", …) to field objects; MSH is numbered as in HL7, so
// "MSH.9.1" is the message code (MSH-1, the field separator, is left out).
// A field object maps component numbers to strings for its first
// repetition; a field that repeats also has "repetitions", one component
// object per repetition in order (an empty repetition is {}). A component
// with subcomponents is an object of subcomponent numbers. Values are
// decoded with the message's own delimiters after splitting, so an escaped
// delimiter is never a separator. Empty fields, components, and
// subcomponents are left out. A message whose MSH-12 version is not 2.1 to
// 2.9 is refused (one without a version is read).
func HL7JSON(in []byte) (map[string]any, error) {
	v, _ := HL7v2().Parse(in) // never fails
	msg := v.(*HL7Message)
	segs := msg.Segments
	if len(segs) == 0 || segs[0].Name != "MSH" || len(segs[0].Field(2)) == 0 { // MSH-2 encoding characters
		return nil, ErrNotHL7
	}
	if ver := strings.TrimSpace(segs[0].Delimiters.Decode(hl7Value(&segs[0], 12))); ver != "" && !hl7Version.MatchString(ver) {
		return nil, &RefusedError{Err: ErrNotHL7, Reason: "unsupported HL7 version (MSH-12 must be 2.1 to 2.9)"}
	}
	doc := map[string]any{}
	var all []any
	for _, seg := range segs {
		obj := map[string]any{"name": seg.Name}
		first := 1 // HL7 number of seg.Fields[0]
		if seg.Name == "MSH" {
			first = 2
		}
		for i, field := range seg.Fields {
			if seg.Name == "MSH" && i == 0 { // MSH-2: the encoding characters themselves
				obj["2"] = componentsJSON(field[0], func(c string) any { return c })
				continue
			}
			if f := fieldJSON(field, seg.Delimiters); f != nil {
				obj[strconv.Itoa(first+i)] = f
			}
		}
		all = append(all, obj)
		if _, seen := doc[seg.Name]; !seen {
			doc[seg.Name] = obj
		}
	}
	doc["segments"] = all
	return doc, nil
}

// fieldJSON is a field's JSON view, nil when every repetition is empty.
// Repetitions keep their positions: an empty one is an empty object, so
// the first repetition's components are always the first repetition's.
func fieldJSON(reps [][]string, d HL7Delimiters) map[string]any {
	value := func(c string) any { return componentJSON(c, d) }
	objs := make([]any, len(reps))
	empty := true
	for i, rep := range reps {
		c := componentsJSON(rep, value)
		objs[i] = c
		empty = empty && len(c) == 0
	}
	if empty {
		return nil
	}
	if len(reps) == 1 {
		return objs[0].(map[string]any)
	}
	out := maps.Clone(objs[0].(map[string]any)) // repetitions holds its own
	out["repetitions"] = objs
	return out
}

// componentsJSON maps component numbers to the non-empty values value
// makes of them (nil and empty objects are empty).
func componentsJSON(comps []string, value func(string) any) map[string]any {
	out := map[string]any{}
	for i, c := range comps {
		switch v := value(c).(type) {
		case string:
			if v != "" {
				out[strconv.Itoa(i+1)] = v
			}
		case map[string]any:
			if len(v) > 0 {
				out[strconv.Itoa(i+1)] = v
			}
		}
	}
	return out
}

// componentJSON is a component as written: its decoded text, or, with
// subcomponents, an object of subcomponent numbers to decoded text.
func componentJSON(c string, d HL7Delimiters) any {
	subs := d.Subcomponents(c)
	if len(subs) == 1 {
		return d.Decode(c)
	}
	out := map[string]any{}
	for i, sub := range subs {
		if sub = d.Decode(sub); sub != "" {
			out[strconv.Itoa(i+1)] = sub
		}
	}
	return out
}
