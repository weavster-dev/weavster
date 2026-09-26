// Package dsl interprets declarative YAML DSL transforms (arch §4.1 Path A)
// over JSON-shaped message documents. Phase 1 runs it natively; Phase 3
// compiles the same package into the embedded WASM interpreter (#107 D-28).
package dsl

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/weavster-dev/weavster/internal/compiler"
)

// ErrUnsupportedStep is returned by Compile for step kinds the interpreter
// does not execute yet.
var ErrUnsupportedStep = errors.New("dsl: step not supported yet")

// Program is a validated transform ready to run.
type Program struct {
	name  string
	steps []step
}

type step interface {
	// apply transforms doc in place and reports whether the message is
	// filtered out.
	apply(doc map[string]any) (filtered bool, err error)
}

// Compile validates t and prepares it for execution.
func Compile(t compiler.Transform) (*Program, error) {
	p := &Program{name: t.Name}
	for i, s := range t.Steps {
		st, err := compileStep(s)
		if err != nil {
			return nil, fmt.Errorf("dsl: %s: step %d: %w", t.Name, i+1, err)
		}
		p.steps = append(p.steps, st)
	}
	return p, nil
}

func compileStep(s compiler.Step) (step, error) {
	set := 0
	for _, present := range []bool{s.Map != nil, s.Set != nil, s.Filter != nil, s.Build != nil, s.DestinationSet != nil} {
		if present {
			set++
		}
	}
	if set != 1 {
		return nil, errors.New("exactly one of map, set, filter, build, destinationSet is required")
	}
	switch {
	case s.Map != nil:
		return compileMap(*s.Map)
	case s.Set != nil:
		return compileSet(*s.Set)
	case s.Filter != nil:
		return compileFilter(*s.Filter)
	case s.Build != nil:
		return nil, fmt.Errorf("build: %w", ErrUnsupportedStep)
	default:
		return nil, fmt.Errorf("destinationSet: %w", ErrUnsupportedStep)
	}
}

// Run applies the program to a copy of in and returns the result; in is
// never modified. A nil in is treated as an empty object. filtered reports
// that a filter step dropped the message; no later steps run.
func (p *Program) Run(in map[string]any) (out map[string]any, filtered bool, err error) {
	doc, _ := deepCopy(in).(map[string]any)
	if doc == nil {
		doc = map[string]any{}
	}
	for i, st := range p.steps {
		dropped, err := st.apply(doc)
		if err != nil {
			return nil, false, fmt.Errorf("dsl: %s: step %d: %w", p.name, i+1, err)
		}
		if dropped {
			return doc, true, nil
		}
	}
	return doc, false, nil
}

// deepCopy copies JSON-shaped values so no two fields share an object or
// array.
func deepCopy(v any) any {
	switch t := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, e := range t {
			m[k] = deepCopy(e)
		}
		return m
	case []any:
		a := make([]any, len(t))
		for i, e := range t {
			a[i] = deepCopy(e)
		}
		return a
	}
	return v
}

// --- paths ---

type path []string

func parsePath(s string) (path, error) {
	if s == "" {
		return nil, errors.New("empty path")
	}
	parts := strings.Split(s, ".")
	if slices.Contains(parts, "") {
		return nil, fmt.Errorf("invalid path %q", s)
	}
	return parts, nil
}

// get returns the value at p, or false when any segment is missing.
// Numeric segments index arrays.
func (p path) get(doc map[string]any) (any, bool) {
	var cur any = doc
	for _, seg := range p {
		switch v := cur.(type) {
		case map[string]any:
			next, ok := v[seg]
			if !ok {
				return nil, false
			}
			cur = next
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(v) {
				return nil, false
			}
			cur = v[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

// set assigns v at p. Segments walk objects, and numeric segments index
// existing arrays; missing intermediates are created as objects. It fails
// when a scalar or an out-of-range index is in the way.
func (p path) set(doc map[string]any, v any) error {
	var cur any = doc
	for i, seg := range p {
		last := i == len(p)-1
		switch c := cur.(type) {
		case map[string]any:
			if last {
				c[seg] = v
				return nil
			}
			next, ok := c[seg]
			if !ok {
				next = map[string]any{}
				c[seg] = next
			}
			cur = next
		case []any:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(c) {
				return fmt.Errorf("cannot set %s: %s has no element %s", strings.Join(p, "."), strings.Join(p[:i], "."), seg)
			}
			if last {
				c[idx] = v
				return nil
			}
			cur = c[idx]
		default:
			return fmt.Errorf("cannot set %s: %s is not an object or array", strings.Join(p, "."), strings.Join(p[:i], "."))
		}
	}
	return nil
}

// --- map ---

type mapStep struct {
	from, to path
	typ      string
}

func compileMap(m compiler.MapStep) (step, error) {
	from, err := parsePath(m.From)
	if err != nil {
		return nil, fmt.Errorf("map.from: %w", err)
	}
	to, err := parsePath(m.To)
	if err != nil {
		return nil, fmt.Errorf("map.to: %w", err)
	}
	switch m.Type {
	case "", "string", "number", "boolean":
	default:
		return nil, fmt.Errorf("map.type must be string, number, or boolean, got %q", m.Type)
	}
	return mapStep{from: from, to: to, typ: m.Type}, nil
}

func (m mapStep) apply(doc map[string]any) (bool, error) {
	v, ok := m.from.get(doc)
	if !ok {
		return false, nil // a missing source leaves the target untouched
	}
	// Copy so later edits to the target never change the source.
	v, err := convert(deepCopy(v), m.typ)
	if err != nil {
		return false, fmt.Errorf("map %s: %w", strings.Join(m.from, "."), err)
	}
	return false, m.to.set(doc, v)
}

// convert applies a map type. null stays null for every type.
func convert(v any, typ string) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch typ {
	case "string":
		return text(v), nil
	case "number":
		switch n := v.(type) {
		case float64:
			return n, nil
		case string:
			f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
			if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
				return nil, fmt.Errorf("%q is not a number", n)
			}
			return f, nil
		}
		return nil, fmt.Errorf("%s is not a number", text(v))
	case "boolean":
		switch b := v.(type) {
		case bool:
			return b, nil
		case string:
			parsed, err := strconv.ParseBool(strings.TrimSpace(b))
			if err != nil {
				return nil, fmt.Errorf("%q is not a boolean", b)
			}
			return parsed, nil
		}
		return nil, fmt.Errorf("%s is not a boolean", text(v))
	}
	return v, nil
}

// text renders a value for templates and string conversion. Objects and
// arrays render as JSON.
func text(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// --- set ---

var placeholder = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)

// templatePart is literal text or, when ref is set, a placeholder.
type templatePart struct {
	literal string
	ref     path
}

type setStep struct {
	field path
	parts []templatePart
}

// compileSet splits the template into literal and placeholder parts once.
func compileSet(s compiler.SetStep) (step, error) {
	field, err := parsePath(s.Field)
	if err != nil {
		return nil, fmt.Errorf("set.field: %w", err)
	}
	var parts []templatePart
	last := 0
	for _, m := range placeholder.FindAllStringSubmatchIndex(s.Expr, -1) {
		p, err := parsePath(s.Expr[m[2]:m[3]])
		if err != nil {
			return nil, fmt.Errorf("set.expr: %w", err)
		}
		parts = append(parts, templatePart{literal: s.Expr[last:m[0]]}, templatePart{ref: p})
		last = m[1]
	}
	parts = append(parts, templatePart{literal: s.Expr[last:]})
	return setStep{field: field, parts: parts}, nil
}

func (s setStep) apply(doc map[string]any) (bool, error) {
	var b strings.Builder
	for _, part := range s.parts {
		if part.ref == nil {
			b.WriteString(part.literal)
			continue
		}
		v, _ := part.ref.get(doc)
		b.WriteString(text(v))
	}
	return false, s.field.set(doc, b.String())
}

// --- filter ---

type operand struct {
	path    path // nil for a literal
	literal any
}

func (o operand) value(doc map[string]any) (any, bool) {
	if o.path != nil {
		return o.path.get(doc)
	}
	return o.literal, true
}

type filterStep struct {
	left, right operand
	op          string // "==", "!=", or "" for a truthy test of left
	reject      bool
}

var (
	comparison = regexp.MustCompile(`^(.+?)\s*(==|!=)\s*(.+)$`)
	// numberLiteral is the only number syntax the DSL accepts; any other
	// unquoted operand is a path, so a field named "inf" stays a field.
	numberLiteral = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)
)

func compileFilter(f compiler.FilterStep) (step, error) {
	var st filterStep
	switch f.Action {
	case "reject":
		st.reject = true
	case "accept":
	default:
		return nil, fmt.Errorf("filter.action must be reject or accept, got %q", f.Action)
	}
	when := strings.TrimSpace(f.When)
	if m := comparison.FindStringSubmatch(when); m != nil {
		left, err := parseOperand(m[1])
		if err != nil {
			return nil, fmt.Errorf("filter.when: %w", err)
		}
		right, err := parseOperand(m[3])
		if err != nil {
			return nil, fmt.Errorf("filter.when: %w", err)
		}
		st.left, st.op, st.right = left, m[2], right
		return st, nil
	}
	left, err := parseOperand(when)
	if err != nil {
		return nil, fmt.Errorf("filter.when: %w (use a path, or <operand> == / != <operand>)", err)
	}
	if left.path == nil {
		return nil, fmt.Errorf("filter.when: %q is a literal; use a path, or <operand> == / != <operand>", when)
	}
	st.left = left
	return st, nil
}

func parseOperand(s string) (operand, error) {
	s = strings.TrimSpace(s)
	switch {
	case len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0]:
		return operand{literal: s[1 : len(s)-1]}, nil
	case s == "true" || s == "false":
		return operand{literal: s == "true"}, nil
	case numberLiteral.MatchString(s):
		f, _ := strconv.ParseFloat(s, 64)
		return operand{literal: f}, nil
	}
	p, err := parsePath(s)
	if err != nil {
		return operand{}, err
	}
	for _, seg := range p {
		if strings.ContainsAny(seg, " '\"=!<>&|()") {
			return operand{}, fmt.Errorf("invalid operand %q", s)
		}
	}
	return operand{path: p}, nil
}

func (f filterStep) apply(doc map[string]any) (bool, error) {
	var cond bool
	switch f.op {
	case "":
		v, ok := f.left.value(doc)
		cond = ok && truthy(v)
	default:
		l, _ := f.left.value(doc)
		r, _ := f.right.value(doc)
		cond = equal(l, r)
		if f.op == "!=" {
			cond = !cond
		}
	}
	// reject drops the message when the condition holds; accept drops it
	// when the condition does not hold.
	return cond == f.reject, nil
}

func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case string:
		return t != ""
	case bool:
		return t
	case float64:
		return t != 0
	}
	return true
}

// equal compares values; a missing value equals "" (so "x == ”" matches an
// absent field), and numbers compare numerically.
func equal(a, b any) bool {
	if af, ok := a.(float64); ok {
		if bf, ok := b.(float64); ok {
			return af == bf
		}
	}
	return text(a) == text(b)
}
