package dsl

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// TestRunRegressions covers review findings: aliasing, arrays, JSON
// rendering, null handling, and number/field ambiguity.
func TestRunRegressions(t *testing.T) {
	tests := []struct {
		name, transform, in, want string
		wantFiltered              bool
	}{
		{"map copies, not aliases", "name: t\nsteps:\n  - map: { from: a, to: b }\n  - set: { field: b.x, expr: hi }",
			`{"a":{}}`, `{"a":{},"b":{"x":"hi"}}`, false},
		{"set into an existing array", "name: t\nsteps:\n  - map: { from: a, to: items.0.x }",
			`{"a":1,"items":[{}]}`, `{"a":1,"items":[{"x":1}]}`, false},
		{"objects render as JSON", "name: t\nsteps:\n  - map: { from: a, to: s, type: string }\n  - set: { field: t, expr: '{{b}}' }",
			`{"a":{"k":1},"b":[1,2]}`, `{"a":{"k":1},"b":[1,2],"s":"{\"k\":1}","t":"[1,2]"}`, false},
		{"null stays null for typed maps", "name: t\nsteps:\n  - map: { from: a, to: b, type: number }",
			`{"a":null}`, `{"a":null,"b":null}`, false},
		{"operator inside a quoted literal", "name: t\nsteps:\n  - filter: { when: \"'a==b' == v\", action: reject }",
			`{"v":"a==b"}`, "", true},
		{"!= inside a quoted literal", "name: t\nsteps:\n  - filter: { when: \"v != \\\"x!=y\\\"\", action: accept }",
			`{"v":"z"}`, `{"v":"z"}`, false},
		{"field named inf is a field", "name: t\nsteps:\n  - filter: { when: \"inf == 'x'\", action: reject }",
			`{"inf":"x"}`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Compile(mustParse(t, tt.transform))
			if err != nil {
				t.Fatal(err)
			}
			out, filtered, err := p.Run(doc(t, tt.in))
			if err != nil || filtered != tt.wantFiltered {
				t.Fatalf("filtered=%v err=%v, want filtered=%v", filtered, err, tt.wantFiltered)
			}
			if !tt.wantFiltered {
				got, _ := json.Marshal(out)
				if string(got) != string(mustJSON(t, tt.want)) {
					t.Errorf("out = %s, want %s", got, tt.want)
				}
			}
		})
	}
}

func mustJSON(t *testing.T, s string) []byte {
	t.Helper()
	b, err := json.Marshal(doc(t, s))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRunDoesNotModifyInput(t *testing.T) {
	p, err := Compile(mustParse(t, "name: t\nsteps:\n  - set: { field: a.x, expr: hi }\n  - map: { from: n, to: m, type: number }"))
	if err != nil {
		t.Fatal(err)
	}
	in := doc(t, `{"a":{},"n":"oops"}`)
	if _, _, err := p.Run(in); err == nil {
		t.Fatal("want conversion error")
	}
	if len(in["a"].(map[string]any)) != 0 {
		t.Errorf("input modified: %v", in)
	}
}

func TestRunNilDocument(t *testing.T) {
	p, err := Compile(mustParse(t, "name: t\nsteps:\n  - set: { field: a, expr: x }"))
	if err != nil {
		t.Fatal(err)
	}
	out, _, err := p.Run(nil)
	if err != nil || out["a"] != "x" {
		t.Errorf("Run(nil) = %v, %v", out, err)
	}
}

func TestRejectsUnsupportedSyntaxAndValues(t *testing.T) {
	for _, when := range []string{"a > 5", "a = 'x'", "(a)", "'lit'", "5"} {
		if _, err := Compile(mustParse(t, "name: t\nsteps:\n  - filter: { when: \""+when+"\", action: accept }")); err == nil {
			t.Errorf("when %q compiled; want an error", when)
		}
	}
	for _, v := range []string{"NaN", "Inf", "-infinity"} {
		p, _ := Compile(mustParse(t, "name: t\nsteps:\n  - map: { from: a, to: b, type: number }"))
		if _, _, err := p.Run(map[string]any{"a": v}); err == nil || !strings.Contains(err.Error(), "is not a number") {
			t.Errorf("%s: err = %v, want not-a-number", v, err)
		}
	}
	pn, _ := Compile(mustParse(t, "name: t\nsteps:\n  - map: { from: a, to: b, type: number }"))
	for _, v := range []float64{math.NaN(), math.Inf(1)} {
		if _, _, err := pn.Run(map[string]any{"a": v}); err == nil || !strings.Contains(err.Error(), "not a finite number") {
			t.Errorf("%v: err = %v, want not-finite", v, err)
		}
	}
	p, _ := Compile(mustParse(t, "name: t\nsteps:\n  - map: { from: a, to: items.5 }"))
	if _, _, err := p.Run(doc(t, `{"a":1,"items":[]}`)); err == nil || !strings.Contains(err.Error(), "has no element 5") {
		t.Errorf("out-of-range array set: err = %v", err)
	}
}

func TestJSONNumbers(t *testing.T) {
	p, err := Compile(mustParse(t, "name: t\nsteps:\n  - filter: { when: n, action: accept }\n  - filter: { when: \"n == 2\", action: accept }\n  - map: { from: n, to: m, type: number }\n  - map: { from: n, to: s, type: string }"))
	if err != nil {
		t.Fatal(err)
	}
	out, filtered, err := p.Run(map[string]any{"n": json.Number("2")})
	if err != nil || filtered || out["m"] != json.Number("2") || out["s"] != "2" {
		t.Errorf("out = %v, filtered = %v, err = %v", out, filtered, err)
	}
	if _, filtered, _ := p.Run(map[string]any{"n": json.Number("0")}); !filtered {
		t.Error("json.Number 0 should be falsy")
	}
}
