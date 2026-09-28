package dsl

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/compiler"
)

func mustParse(t *testing.T, yaml string) compiler.Transform {
	t.Helper()
	tr, err := compiler.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return *tr
}

func doc(t *testing.T, js string) map[string]any {
	t.Helper()
	var d map[string]any
	if err := json.Unmarshal([]byte(js), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

// archExample is the arch §4.1 example, adapted to JSON paths.
const archExample = `
kind: Transform
name: normalize-patient-name
inputs: [message]
steps:
  - map: { from: "PID.5.1", to: "patient.lastName", type: string }
  - map: { from: "PID.5.2", to: "patient.firstName", type: string }
  - set: { field: "patient.fullName", expr: "{{patient.lastName}}, {{patient.firstName}}" }
  - filter: { when: "patient.lastName == ''", action: reject }
`

func TestRun(t *testing.T) {
	tests := []struct {
		name         string
		transform    string
		in           string
		want         string // expected document; "" when filtered
		wantFiltered bool
	}{
		{
			name:      "arch example passes",
			transform: archExample,
			in:        `{"PID":{"5":{"1":"Doe","2":"John"}}}`,
			want:      `{"PID":{"5":{"1":"Doe","2":"John"}},"patient":{"lastName":"Doe","firstName":"John","fullName":"Doe, John"}}`,
		},
		{
			name:         "arch example rejects a missing last name",
			transform:    archExample,
			in:           `{"PID":{"5":{"2":"John"}}}`,
			wantFiltered: true,
		},
		{
			name: "map with array index and number/boolean conversion",
			transform: `name: t
steps:
  - map: { from: "items.1.qty", to: "qty", type: number }
  - map: { from: "flags.active", to: "active", type: boolean }
  - map: { from: "count", to: "countText", type: string }`,
			in:   `{"items":[{"qty":"1"},{"qty":" 2.5 "}],"flags":{"active":"true"},"count":3}`,
			want: `{"items":[{"qty":"1"},{"qty":" 2.5 "}],"flags":{"active":"true"},"count":3,"qty":2.5,"active":true,"countText":"3"}`,
		},
		{
			name: "missing map source leaves target untouched",
			transform: `name: t
steps:
  - map: { from: "nope", to: "x" }`,
			in:   `{"x":1}`,
			want: `{"x":1}`,
		},
		{
			name: "set with literal text and missing placeholder",
			transform: `name: t
steps:
  - set: { field: "greeting", expr: "Hello {{ name }}{{missing}}!" }
  - set: { field: "a.b.c", expr: "static" }`,
			in:   `{"name":"Ada"}`,
			want: `{"name":"Ada","greeting":"Hello Ada!","a":{"b":{"c":"static"}}}`,
		},
		{
			name: "accept keeps matching messages",
			transform: `name: t
steps:
  - filter: { when: "type == \"ADT\"", action: accept }`,
			in:   `{"type":"ADT"}`,
			want: `{"type":"ADT"}`,
		},
		{
			name: "accept drops non-matching messages",
			transform: `name: t
steps:
  - filter: { when: "type == 'ADT'", action: accept }`,
			in:           `{"type":"ORM"}`,
			wantFiltered: true,
		},
		{
			name: "numeric and boolean comparisons",
			transform: `name: t
steps:
  - filter: { when: "count != 3", action: reject }
  - filter: { when: "urgent == true", action: accept }`,
			in:   `{"count":3,"urgent":true}`,
			want: `{"count":3,"urgent":true}`,
		},
		{
			name: "path to path comparison",
			transform: `name: t
steps:
  - filter: { when: "a == b", action: reject }`,
			in:           `{"a":"x","b":"x"}`,
			wantFiltered: true,
		},
		{
			name: "truthy test rejects when present",
			transform: `name: t
steps:
  - filter: { when: "test.flag", action: reject }`,
			in:           `{"test":{"flag":true}}`,
			wantFiltered: true,
		},
		{
			name: "truthy test keeps falsy values",
			transform: `name: t
steps:
  - filter: { when: "flag", action: reject }
  - filter: { when: "zero", action: reject }
  - filter: { when: "empty", action: reject }
  - filter: { when: "null", action: reject }
  - filter: { when: "obj", action: accept }`,
			in:   `{"flag":false,"zero":0,"empty":"","null":null,"obj":{}}`,
			want: `{"flag":false,"zero":0,"empty":"","null":null,"obj":{}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Compile(mustParse(t, tt.transform))
			if err != nil {
				t.Fatal(err)
			}
			out, filtered, err := run(p, doc(t, tt.in))
			if err != nil {
				t.Fatal(err)
			}
			if filtered != tt.wantFiltered {
				t.Fatalf("filtered = %v, want %v", filtered, tt.wantFiltered)
			}
			if !tt.wantFiltered && !reflect.DeepEqual(out, doc(t, tt.want)) {
				got, _ := json.Marshal(out)
				t.Errorf("out = %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestCompileErrors(t *testing.T) {
	tests := []struct {
		name, transform, want string
	}{
		{"build not last", "name: t\nsteps:\n  - build: { template: '{}' }\n  - set: { field: a, expr: b }", "step 1: build must be the last step"},
		{"build format", "name: t\nsteps:\n  - build: { template: x, format: csv }", "build.format must be json, hl7v2, xml, or text"},
		{"build empty", "name: t\nsteps:\n  - build: { template: '  ' }", "build.template is empty"},
		{"build bad placeholder", "name: t\nsteps:\n  - build: { template: '{{a..b}}' }", "build.template: invalid path"},
		{"build json unquoted", "name: t\nsteps:\n  - build: { template: '{\"n\": {{n}}}' }", "every {{path}} must be inside a string"},
		{"build json after a string", "name: t\nsteps:\n  - build: { template: '{\"a\": \"x\", \"n\": {{n}}}' }", "every {{path}} must be inside a string"},
		{"build hl7 without MSH", "name: t\nsteps:\n  - build: { format: hl7v2, template: 'PID|1' }", "must start with MSH|^~\\&"},
		{"build hl7 custom delimiters", "name: t\nsteps:\n  - build: { format: hl7v2, template: 'MSH*^~\\&*A' }", "must start with MSH|^~\\&"},
		{"destinationSet include", "name: t\nsteps:\n  - destinationSet: { include: [a] }", "destinationSet.include is not supported"},
		{"destinationSet empty", "name: t\nsteps:\n  - destinationSet: { exclude: [] }", "destinationSet.exclude must name at least one destination"},
		{"destinationSet bad when", "name: t\nsteps:\n  - destinationSet: { exclude: [a], when: 'a == b == c' }", "destinationSet.when: invalid operand"},
		{"empty step", "name: t\nsteps:\n  - {}", "exactly one of"},
		{"two kinds in one step", "name: t\nsteps:\n  - map: { from: a, to: b }\n    set: { field: c, expr: d }", "exactly one of"},
		{"bad map from", "name: t\nsteps:\n  - map: { from: 'a..b', to: b }", "map.from: invalid path"},
		{"bad map to", "name: t\nsteps:\n  - map: { from: a, to: '' }", "map.to: empty path"},
		{"bad map type", "name: t\nsteps:\n  - map: { from: a, to: b, type: date }", "map.type must be"},
		{"bad set field", "name: t\nsteps:\n  - set: { field: '', expr: x }", "set.field"},
		{"bad placeholder", "name: t\nsteps:\n  - set: { field: a, expr: '{{ }}' }", "set.expr"},
		{"bad filter action", "name: t\nsteps:\n  - filter: { when: a, action: drop }", "filter.action"},
		{"bad filter path", "name: t\nsteps:\n  - filter: { when: '', action: reject }", "filter.when"},
		{"bad left operand", "name: t\nsteps:\n  - filter: { when: 'a..b == 1', action: reject }", "filter.when"},
		{"bad right operand", "name: t\nsteps:\n  - filter: { when: \"a == b c\", action: reject }", "invalid operand"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Compile(mustParse(t, tt.transform))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestRunErrors(t *testing.T) {
	tests := []struct {
		name, transform, in, want string
	}{
		{"not a number", "name: t\nsteps:\n  - map: { from: a, to: b, type: number }", `{"a":"x"}`, `"x" is not a number`},
		{"object not a number", "name: t\nsteps:\n  - map: { from: a, to: b, type: number }", `{"a":{}}`, "is not a number"},
		{"not a boolean", "name: t\nsteps:\n  - map: { from: a, to: b, type: boolean }", `{"a":"maybe"}`, `"maybe" is not a boolean`},
		{"number not a boolean", "name: t\nsteps:\n  - map: { from: a, to: b, type: boolean }", `{"a":1}`, "is not a boolean"},
		{"set through a scalar", "name: t\nsteps:\n  - set: { field: a.b, expr: x }", `{"a":1}`, "a is not an object"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Compile(mustParse(t, tt.transform))
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := run(p, doc(t, tt.in)); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestPathLookupEdges(t *testing.T) {
	d := doc(t, `{"list":[1,2],"scalar":"s"}`)
	for _, p := range []string{"list.5", "list.-1", "list.x", "scalar.child"} {
		if _, ok := path(strings.Split(p, ".")).get(d); ok {
			t.Errorf("get(%s) found a value", p)
		}
	}
	if got := text([]any{1.0}); got != "[1]" {
		t.Errorf("text(slice) = %q", got)
	}
}

// TestDestinationSet: steps exclude destinations when their condition
// holds (always without one), accumulate in order, and a filter that drops
// the message returns no exclusions.
func TestDestinationSet(t *testing.T) {
	prog := compileYAML(t, `name: t
steps:
  - destinationSet: { exclude: [archive] }
  - destinationSet: { exclude: [ehr, lab], when: "kind == 'orm'" }
  - set: { field: routed, expr: yes }
  - destinationSet: { exclude: [lab], when: routed }
`)
	for _, tt := range []struct {
		in   map[string]any
		want string
	}{
		{map[string]any{"kind": "adt"}, "archive,lab"},
		{map[string]any{"kind": "orm"}, "archive,ehr,lab"},
	} {
		o, err := prog.Execute(tt.in)
		if err != nil || o.Filtered || strings.Join(o.Excluded, ",") != tt.want || o.Doc["routed"] != "yes" {
			t.Errorf("%v: %+v, err %v; want %s", tt.in, o, err, tt.want)
		}
	}
	dropped := compileYAML(t, "name: t\nsteps:\n  - destinationSet: { exclude: [a] }\n  - filter: { when: x, action: accept }\n")
	if o, err := dropped.Execute(map[string]any{}); err != nil || !o.Filtered || o.Excluded != nil {
		t.Errorf("filtered message: %+v %v", o, err)
	}

}

// compileYAML compiles a transform written in YAML.
func compileYAML(t *testing.T, yaml string) *Program {
	t.Helper()
	p, err := Compile(mustParse(t, yaml))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestBuild: the build step renders the template with values escaped for
// the format, checks the result, and Run refuses it.
func TestBuild(t *testing.T) {
	doc := map[string]any{"last": "O'BRIEN|JR", "note": "a < b & \"c\"\nnext", "n": json.Number("42")}
	for _, tt := range []struct {
		name, step, want, err string
	}{
		{"json", `build: { template: '{"name": "{{last}}", "note": "{{note}}", "n": "{{n}}", "q": "\"{{last}}\""}' }`,
			`{"name": "O'BRIEN|JR", "note": "a \u003c b \u0026 \"c\"\nnext", "n": "42", "q": "\"O'BRIEN|JR\""}`, ""},
		{"hl7v2", "build: { format: hl7v2, template: \"MSH|^~\\\\&|W|H|LAB|H|20260927||ADT^A01|1|P|2.5\\nPID|1||123||{{last}}\\nNTE|1||{{note}}\\n\" }",
			"MSH|^~\\&|W|H|LAB|H|20260927||ADT^A01|1|P|2.5\rPID|1||123||O'BRIEN\\F\\JR\rNTE|1||a < b \\T\\ \"c\"\\X0A\\next\r", ""},
		{"xml", `build: { format: xml, template: '<p name="{{last}}"><note>{{note}}</note></p>' }`,
			"<p name=\"O&apos;BRIEN|JR\"><note>a &lt; b &amp; &quot;c&quot;\nnext</note></p>", ""},
		{"text", `build: { format: text, template: 'Dear {{last}}' }`, "Dear O'BRIEN|JR", ""},
		{"json not an object", `build: { template: '"{{last}}"' }`, "", "the result is not a JSON object"},
		{"json broken", `build: { template: '{"a": "{{last}}"' }`, "", "the result is not a JSON object"},
		{"xml other encoding", `build: { format: xml, template: '<?xml version="1.0" encoding="ISO-8859-1"?><p>{{last}}</p>' }`, "", `the XML declaration says encoding "ISO-8859-1"`},
		{"xml broken", `build: { format: xml, template: '<a>{{last}}' }`, "", "the result is not a well-formed XML document"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			prog := compileYAML(t, "name: t\nsteps:\n  - set: { field: seen, expr: yes }\n  - "+tt.step+"\n")
			o, err := prog.Execute(doc)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) || !strings.Contains(err.Error(), "step 2: build") {
					t.Errorf("err = %v, want %q", err, tt.err)
				}
				return
			}
			if err != nil || string(o.Body) != tt.want || o.Format != tt.name || o.Doc["seen"] != "yes" {
				t.Errorf("got %q (%s), %v\nwant %q", o.Body, o.Format, err, tt.want)
			}
		})
	}
	framed, err := compileYAML(t, "name: t\nsteps:\n  - build: { format: hl7v2, template: \"MSH|^~\\\\&|{{v}}\" }\n").Execute(map[string]any{"v": "a\x0b\x1c\rb"})
	if err != nil || string(framed.Body) != "MSH|^~\\&|a\\X0B\\\\X1C\\\\X0D\\b\r" {
		t.Errorf("framing bytes: %q %v", framed.Body, err)
	}
	if p := compileYAML(t, "name: t\nsteps:\n  - set: { field: a, expr: b }\n"); p.Format() != "" {
		t.Errorf("Format without build = %q", p.Format())
	}
	if p := compileYAML(t, "name: t\nsteps:\n  - build: { template: '{}' }\n"); p.Format() != "json" {
		t.Errorf("Format of a default build = %q", p.Format())
	}
	if o, err := compileYAML(t, "name: t\nsteps:\n  - filter: { when: x, action: accept }\n  - build: { template: '{}' }\n").Execute(nil); err != nil || !o.Filtered || o.Body != nil {
		t.Errorf("filtered before build: %+v %v", o, err)
	}
}

// run is Execute returning the document and whether it was filtered.
func run(p *Program, in map[string]any) (map[string]any, bool, error) {
	o, err := p.Execute(in)
	return o.Doc, o.Filtered, err
}
