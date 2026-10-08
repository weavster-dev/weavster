package pipeline

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/compiler"
)

func transformJSON(t *testing.T, js string) *compiler.Transform {
	t.Helper()
	var tr compiler.Transform
	if err := json.Unmarshal([]byte(js), &tr); err != nil {
		t.Fatal(err)
	}
	return &tr
}

// TestRun: what processing would make of a message, without a store:
// passthrough, filter, output and exclusions, destination transforms that
// transform, filter, or fail, a build step's format, and an unreadable
// input.
func TestRun(t *testing.T) {
	f := Flow{ID: "a",
		Transform: transformJSON(t, `{"steps":[{"filter":{"when":"keep == 'no'","action":"reject"}},{"map":{"from":"a","to":"b"}},{"destinationSet":{"exclude":["x"],"when":"b == 2"}}]}`),
		Destinations: []Destination{
			{Name: "json", Type: "file", Transform: transformJSON(t, `{"steps":[{"map":{"from":"b","to":"c"}}]}`)},
			{Name: "drop", Type: "file", Transform: transformJSON(t, `{"steps":[{"filter":{"when":"b == 1","action":"reject"}}]}`)},
			{Name: "hl7", Type: "file", Transform: transformJSON(t, `{"steps":[{"build":{"format":"hl7v2","template":"MSH|^~\\&|A|B|C|D|||ADT^A01|{{b}}|P|2.5"}}]}`)},
			{Name: "x", Type: "file", Transform: transformJSON(t, `{"steps":[{"map":{"from":"b","to":"c"}}]}`)},
			{Name: "plain", Type: "file"},
		}}
	text := Flow{ID: "t", Transform: transformJSON(t, `{"steps":[{"build":{"format":"text","template":"hello {{a}}"}}]}`),
		Destinations: []Destination{{Name: "d", Type: "file", Transform: transformJSON(t, `{"steps":[{"map":{"from":"a","to":"b"}}]}`)}}}
	for _, tt := range []struct {
		name  string
		flow  Flow
		input string
		check func(RunResult) bool
	}{
		{"transformed", f, `{"a":1}`, func(r RunResult) bool {
			return r.Status == "transformed" && r.ContentType == "json" && string(r.Output) == `{"a":1,"b":1}` && len(r.Excluded) == 0
		}},
		{"destination transform", f, `{"a":1}`, func(r RunResult) bool {
			d := r.Destinations["json"]
			return d.Status == "transformed" && string(d.Output) == `{"a":1,"b":1,"c":1}` && d.ContentType == "json"
		}},
		{"destination filter", f, `{"a":1}`, func(r RunResult) bool { return r.Destinations["drop"].Status == "filtered" }},
		{"destination build", f, `{"a":1}`, func(r RunResult) bool {
			d := r.Destinations["hl7"]
			return d.Status == "transformed" && d.ContentType == "hl7v2" && strings.Contains(string(d.Output), "ADT^A01|1|P")
		}},
		{"no destination transform", f, `{"a":1}`, func(r RunResult) bool { _, ok := r.Destinations["plain"]; return !ok }},
		{"excluded", f, `{"a":2}`, func(r RunResult) bool {
			_, ran := r.Destinations["x"]
			return len(r.Excluded) == 1 && r.Excluded[0] == "x" && !ran
		}},
		{"filtered", f, `{"keep":"no"}`, func(r RunResult) bool { return r.Status == "filtered" && r.Output == nil }},
		{"unreadable", f, `not json`, func(r RunResult) bool { return r.Status == "errored" && strings.Contains(r.Error, "JSON object") }},
		{"passthrough", Flow{ID: "p"}, "raw bytes", func(r RunResult) bool {
			return r.Status == "transformed" && r.ContentType == "raw" && string(r.Output) == "raw bytes"
		}},
		{"text output to a destination transform", text, `{"a":1}`, func(r RunResult) bool {
			return r.Destinations["d"].Status == "errored" && strings.Contains(r.Destinations["d"].Error, "text")
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if r := Run(tt.flow, []byte(tt.input)); !tt.check(r) {
				t.Errorf("Run = %+v (output %s)", r, r.Output)
			}
		})
	}
}
