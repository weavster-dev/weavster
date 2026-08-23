// Package compiler transpiles declarative YAML DSL transforms into Go+TinyGo
// source compiled to WASI.
package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Result is the compiler output: the generated Go source and the digest of the
// source-of-truth YAML (the cached .wasm artifact key).
type Result struct {
	GoSource []byte
	Digest   string
}

// Parse decodes a YAML transform into its AST. YAML is the source of truth
// (arch §4.1).
func Parse(data []byte) (*Transform, error) {
	var t Transform
	if err := yaml.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("compiler: parse transform: %w", err)
	}
	if t.Kind == "" {
		t.Kind = "Transform"
	}
	return &t, nil
}

// Compile parses, validates, and transpiles a YAML transform to Go source,
// returning the transform and the generated code (arch §4.1).
func Compile(data []byte) (*Transform, *Result, error) {
	t, err := Parse(data)
	if err != nil {
		return nil, nil, err
	}
	if err := Validate(data); err != nil {
		return nil, nil, err
	}
	src, err := Generate(t)
	if err != nil {
		return nil, nil, err
	}
	sum := sha256.Sum256(data)
	return t, &Result{GoSource: src, Digest: hex.EncodeToString(sum[:])}, nil
}
