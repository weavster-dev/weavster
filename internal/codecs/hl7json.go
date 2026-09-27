package codecs

import (
	"errors"
	"strconv"
	"strings"
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
// object per non-empty repetition. Empty fields and components are left out.
func HL7JSON(in []byte) (map[string]any, error) {
	text := strings.TrimLeft(normalizeSegTerminators(string(in)), "\r")
	if len(text) < 4 || !strings.HasPrefix(text, "MSH") {
		return nil, ErrNotHL7
	}
	v, err := HL7v2().Parse(in)
	if err != nil {
		return nil, err
	}
	doc := map[string]any{}
	var all []any
	for _, seg := range v.(*HL7Message).Segments {
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

// fieldJSON is a field's JSON view, nil when the field is empty.
func fieldJSON(reps [][]string) map[string]any {
	var objs []any
	for _, rep := range reps {
		if c := componentsJSON(rep); c != nil {
			objs = append(objs, c)
		}
	}
	if len(objs) == 0 {
		return nil
	}
	out := objs[0].(map[string]any)
	if len(objs) > 1 {
		copyOut := make(map[string]any, len(out)+1)
		for k, v := range out {
			copyOut[k] = v
		}
		copyOut["repetitions"] = objs
		return copyOut
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
	if len(out) == 0 {
		return nil
	}
	return out
}
