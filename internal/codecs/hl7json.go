package codecs

import (
	"errors"
	"strconv"
)

// ErrNotHL7 reports input that is not an HL7 v2 message (no MSH segment
// first).
var ErrNotHL7 = errors.New("not an HL7 v2 message (no MSH segment first)")

// HL7JSON parses an HL7 v2 message into the JSON view the DSL reads (#107
// D-61): each segment name holds its first occurrence, and "segments" holds
// every segment in order (each with its "name"). A segment maps HL7 field
// numbers ("1", "2", …) to field objects; MSH is numbered as in HL7, so
// "MSH.9.1" is the message code (MSH-1, the field separator, is left out).
// A field object maps component numbers to strings for its first
// repetition; a field that repeats also has "repetitions", one component
// object per repetition in order (an empty repetition is {}). Empty fields
// and components are left out.
func HL7JSON(in []byte) (map[string]any, error) {
	v, _ := HL7v2().Parse(in) // never fails
	segs := v.(*HL7Message).Segments
	if len(segs) == 0 || segs[0].Name != "MSH" || len(segs[0].Field(2)) == 0 { // MSH-2 encoding characters
		return nil, ErrNotHL7
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
			if f := fieldJSON(field); f != nil {
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
func fieldJSON(reps [][]string) map[string]any {
	objs := make([]any, len(reps))
	empty := true
	for i, rep := range reps {
		c := componentsJSON(rep)
		objs[i] = c
		empty = empty && len(c) == 0
	}
	if empty {
		return nil
	}
	out := componentsJSON(reps[0]) // a copy: repetitions holds its own
	if len(reps) > 1 {
		out["repetitions"] = objs
	}
	return out
}

// componentsJSON maps component numbers to non-empty values.
func componentsJSON(comps []string) map[string]any {
	out := map[string]any{}
	for i, c := range comps {
		if c != "" {
			out[strconv.Itoa(i+1)] = c
		}
	}
	return out
}
