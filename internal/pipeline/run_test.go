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
	r := Run(f, []byte(`{"a":1}`))
	if r.Status != "transformed" || r.ContentType != "json" || string(r.Output) != `{"a":1,"b":1}` || len(r.Excluded) != 0 {
		t.Errorf("run = %+v %s", r, r.Output)
	}
	if d := r.Destinations["json"]; d.Status != "transformed" || string(d.Output) != `{"a":1,"b":1,"c":1}` || d.ContentType != "json" {
		t.Errorf("json = %+v %s", d, d.Output)
	}
	if d := r.Destinations["drop"]; d.Status != "filtered" {
		t.Errorf("drop = %+v", d)
	}
	if d := r.Destinations["hl7"]; d.Status != "transformed" || d.ContentType != "hl7v2" || !strings.Contains(string(d.Output), "ADT^A01|1|P") {
		t.Errorf("hl7 = %+v %q", d, d.Output)
	}
	if _, ok := r.Destinations["plain"]; ok {
		t.Error("a destination without a transform has a result")
	}

	if r := Run(f, []byte(`{"a":2}`)); len(r.Excluded) != 1 || r.Excluded[0] != "x" || r.Destinations["x"].Status != "" {
		t.Errorf("excluded = %+v", r)
	}
	if r := Run(f, []byte(`{"keep":"no"}`)); r.Status != "filtered" || r.Output != nil {
		t.Errorf("filtered = %+v", r)
	}
	if r := Run(f, []byte(`not json`)); r.Status != "errored" || !strings.Contains(r.Error, "JSON object") {
		t.Errorf("errored = %+v", r)
	}
	if r := Run(Flow{ID: "p"}, []byte("raw bytes")); r.Status != "transformed" || r.ContentType != "raw" || string(r.Output) != "raw bytes" {
		t.Errorf("passthrough = %+v", r)
	}
	text := Flow{ID: "t", Transform: transformJSON(t, `{"steps":[{"build":{"format":"text","template":"hello {{a}}"}}]}`),
		Destinations: []Destination{{Name: "d", Type: "file", Transform: transformJSON(t, `{"steps":[{"map":{"from":"a","to":"b"}}]}`)}}}
	if r := Run(text, []byte(`{"a":1}`)); r.Destinations["d"].Status != "errored" || !strings.Contains(r.Destinations["d"].Error, "text") {
		t.Errorf("text output = %+v", r)
	}
}
