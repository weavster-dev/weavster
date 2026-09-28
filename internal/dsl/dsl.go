// Package dsl interprets declarative YAML DSL transforms (arch §4.1 Path A)
// over JSON-shaped message documents. Phase 1 runs it natively; Phase 3
// compiles the same package into the embedded WASM interpreter (#107 D-28).
package dsl

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"slices"
	"sort"
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
	// routes: the program has destinationSet steps, which only a flow's
	// transform may use (RunRouted); Run refuses it.
	routes bool
}

type step interface {
	// apply transforms doc in place, adds to excluded the destinations the
	// message skips, and reports whether the message is filtered out.
	apply(doc map[string]any, excluded map[string]bool) (filtered bool, err error)
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
		if _, ok := st.(destinationSetStep); ok {
			p.routes = true
		}
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
		return compileDestinationSet(*s.DestinationSet)
	}
}

// ErrRoutesElsewhere is returned by Run for a program with destinationSet
// steps: only a flow's transform decides destinations (RunRouted).
var ErrRoutesElsewhere = errors.New("destinationSet can only be used in a flow's transform")

// Run applies the program to a copy of in and returns the result; in is
// never modified. A nil in is treated as an empty object. filtered reports
// that a filter step dropped the message; no later steps run. A program
// with destinationSet steps is refused (ErrRoutesElsewhere), so exclusions
// are never silently dropped.
func (p *Program) Run(in map[string]any) (out map[string]any, filtered bool, err error) {
	if p.routes {
		return nil, false, fmt.Errorf("dsl: %s: %w", p.name, ErrRoutesElsewhere)
	}
	out, filtered, _, err = p.RunRouted(in)
	return out, filtered, err
}

// RunRouted is Run that also returns, sorted, the destinations its
// destinationSet steps excluded for this message.
func (p *Program) RunRouted(in map[string]any) (out map[string]any, filtered bool, excluded []string, err error) {
	doc, _ := deepCopy(in).(map[string]any)
	if doc == nil {
		doc = map[string]any{}
	}
	skip := map[string]bool{}
	for i, st := range p.steps {
		dropped, err := st.apply(doc, skip)
		if err != nil {
			return nil, false, nil, fmt.Errorf("dsl: %s: step %d: %w", p.name, i+1, err)
		}
		if dropped {
			return doc, true, nil, nil
		}
	}
	for name := range skip {
		excluded = append(excluded, name)
	}
	sort.Strings(excluded)
	return doc, false, excluded, nil
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

func (m mapStep) apply(doc map[string]any, _ map[string]bool) (bool, error) {
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
			if math.IsNaN(n) || math.IsInf(n, 0) {
				return nil, fmt.Errorf("%v is not a finite number", n)
			}
			return n, nil
		case json.Number:
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
	case json.Number:
		return t.String()
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

func (s setStep) apply(doc map[string]any, _ map[string]bool) (bool, error) {
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

// condition is a when expression: a truthy path, or two operands compared
// with == or !=.
type condition struct {
	left, right operand
	op          string // "==", "!=", or "" for a truthy test of left
}

type filterStep struct {
	when   condition
	reject bool
}

// splitComparison finds the first == or != outside quoted strings.
func splitComparison(when string) (left, op, right string, ok bool) {
	var quote byte
	for i := 0; i+1 < len(when); i++ {
		c := when[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case (c == '=' || c == '!') && when[i+1] == '=':
			return when[:i], when[i : i+2], when[i+2:], true
		}
	}
	return "", "", "", false
}

var (
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
	cond, err := parseCondition(f.When)
	if err != nil {
		return nil, fmt.Errorf("filter.when: %w", err)
	}
	st.when = cond
	return st, nil
}

// parseCondition parses a when expression.
func parseCondition(when string) (condition, error) {
	when = strings.TrimSpace(when)
	if l, op, rt, ok := splitComparison(when); ok {
		left, err := parseOperand(l)
		if err != nil {
			return condition{}, err
		}
		right, err := parseOperand(rt)
		if err != nil {
			return condition{}, err
		}
		return condition{left: left, op: op, right: right}, nil
	}
	left, err := parseOperand(when)
	if err != nil {
		return condition{}, fmt.Errorf("%w (use a path, or <operand> == / != <operand>)", err)
	}
	if left.path == nil {
		return condition{}, fmt.Errorf("%q is a literal; use a path, or <operand> == / != <operand>", when)
	}
	return condition{left: left}, nil
}

// destinationSetStep excludes destinations for the message when its
// condition holds (always without one), #107 D-20.
type destinationSetStep struct {
	when    *condition
	exclude []string
}

func compileDestinationSet(d compiler.DestinationSetStep) (step, error) {
	switch {
	case len(d.Include) > 0:
		return nil, errors.New("destinationSet.include is not supported: destinations can only be excluded")
	case len(d.Exclude) == 0:
		return nil, errors.New("destinationSet.exclude must name at least one destination")
	}
	st := destinationSetStep{exclude: d.Exclude}
	if strings.TrimSpace(d.When) != "" {
		cond, err := parseCondition(d.When)
		if err != nil {
			return nil, fmt.Errorf("destinationSet.when: %w", err)
		}
		st.when = &cond
	}
	return st, nil
}

func (d destinationSetStep) apply(doc map[string]any, excluded map[string]bool) (bool, error) {
	if d.when == nil || d.when.holds(doc) {
		for _, name := range d.exclude {
			excluded[name] = true
		}
	}
	return false, nil
}

func parseOperand(s string) (operand, error) {
	s = strings.TrimSpace(s)
	switch {
	case len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0]:
		return operand{literal: s[1 : len(s)-1]}, nil
	case s == "true" || s == "false":
		return operand{literal: s == "true"}, nil
	case numberLiteral.MatchString(s):
		return operand{literal: json.Number(s)}, nil // exact, like document numbers
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

// holds evaluates the condition on doc.
func (c condition) holds(doc map[string]any) bool {
	if c.op == "" {
		v, ok := c.left.value(doc)
		return ok && truthy(v)
	}
	l, _ := c.left.value(doc)
	r, _ := c.right.value(doc)
	return equal(l, r) == (c.op == "==")
}

func (f filterStep) apply(doc map[string]any, _ map[string]bool) (bool, error) {
	// reject drops the message when the condition holds; accept drops it
	// when the condition does not hold.
	return f.when.holds(doc) == f.reject, nil
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
	case json.Number:
		n, ok := number(t)
		return !ok || n.Sign() != 0
	}
	return true
}

// equal compares values; a missing value equals "" (so "x == ”" matches an
// absent field), and numbers compare numerically.
func equal(a, b any) bool {
	if an, ok := number(a); ok {
		if bn, ok := number(b); ok {
			return an.Cmp(bn) == 0
		}
	}
	return text(a) == text(b)
}

// number returns a numeric value exactly (documents decoded with
// json.Decoder.UseNumber hold json.Number), so large identifiers and tiny
// fractions compare without float64 rounding.
func number(v any) (*big.Float, bool) {
	switch n := v.(type) {
	case float64:
		return new(big.Float).SetFloat64(n), !math.IsNaN(n) && !math.IsInf(n, 0)
	case json.Number:
		f, ok := new(big.Float).SetPrec(1024).SetString(n.String())
		return f, ok
	}
	return nil, false
}
