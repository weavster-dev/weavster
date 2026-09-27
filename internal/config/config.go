// Package config implements config-as-code: JSON-Schema validation, plan,
// apply, and drift detection.
package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/weavster-dev/weavster/internal/artifact"
	"github.com/weavster-dev/weavster/internal/flowdef"
)

// Config is the root config-as-code document: the single source of truth for
// flows, alerts, snippets, snippet libraries, global scripts, the config
// map, and settings (arch §6, #107 D-46). Each section is keyed by the
// artifact's id or name; an artifact that repeats it must match its key.
type Config struct {
	Version string `json:"version" yaml:"version"`
	// Flows are flow definitions keyed by flow id: the same model the flow
	// API accepts (flow.schema.json).
	Flows            map[string]flowdef.Flow            `json:"flows" yaml:"-"` // decoded through JSON (parse)
	Alerts           map[string]artifact.Alert          `json:"alerts" yaml:"alerts"`
	Snippets         map[string]artifact.Snippet        `json:"snippets" yaml:"snippets"`
	SnippetLibraries map[string]artifact.SnippetLibrary `json:"snippetLibraries" yaml:"snippetLibraries"`
	// Scripts and ConfigMap hold text values; Settings any JSON value.
	Scripts   map[string]string `json:"scripts" yaml:"scripts"`
	ConfigMap map[string]string `json:"configmap" yaml:"configmap"`
	Settings  map[string]any    `json:"settings" yaml:"settings"`
}

// Parse decodes a config document. YAML is the canonical format; JSON is a
// YAML subset and is accepted as-is (arch §6). Unknown fields anywhere are
// errors. An artifact's id (flows, alerts) or name (snippets, libraries)
// defaults to its key and must match it when set.
func Parse(data []byte) (*Config, error) {
	c, _, err := parse(data)
	return c, err
}

// goTypeName matches the Go type yaml names in errors.
var goTypeName = regexp.MustCompile(` in type [\w.]+`)

// document is Config as written; flows stay YAML nodes so each is decoded
// as a flow definition through JSON (transforms stay JSON objects).
type document struct {
	Version          string                             `yaml:"version"`
	Flows            map[string]yaml.Node               `yaml:"flows"`
	Alerts           map[string]artifact.Alert          `yaml:"alerts"`
	Snippets         map[string]artifact.Snippet        `yaml:"snippets"`
	SnippetLibraries map[string]artifact.SnippetLibrary `yaml:"snippetLibraries"`
	Scripts          map[string]string                  `yaml:"scripts"`
	ConfigMap        map[string]string                  `yaml:"configmap"`
	Settings         map[string]any                     `yaml:"settings"`
}

// parse decodes the document strictly. It also returns each flow as
// written, for validation against the flow schema.
func parse(data []byte) (*Config, map[string]json.RawMessage, error) {
	var doc document
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	err := dec.Decode(&doc)
	if errors.Is(err, io.EOF) {
		return nil, nil, errors.New("config: the document is empty")
	}
	if err != nil {
		// "field x not found in type config.document": name the field only.
		return nil, nil, fmt.Errorf("config: parse: %s", goTypeName.ReplaceAllString(err.Error(), ""))
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, nil, errors.New("config: parse: the file holds more than one YAML document (---); put everything in one")
	}
	if doc.Version != "" && doc.Version != "1" {
		return nil, nil, fmt.Errorf("config: version %q is not supported; use \"1\"", doc.Version)
	}
	c := &Config{Version: doc.Version, Alerts: doc.Alerts, Snippets: doc.Snippets, SnippetLibraries: doc.SnippetLibraries,
		Scripts: doc.Scripts, ConfigMap: doc.ConfigMap, Settings: doc.Settings}
	if c.Version == "" {
		c.Version = "1"
	}
	normalize(c)
	for key, a := range c.Alerts {
		if err := keyed(&a, "alerts", key); err != nil {
			return nil, nil, err
		}
		c.Alerts[key] = a
	}
	for key, sn := range c.Snippets {
		if err := keyed(&sn, "snippets", key); err != nil {
			return nil, nil, err
		}
		c.Snippets[key] = sn
	}
	for key, l := range c.SnippetLibraries {
		if err := keyed(&l, "snippetLibraries", key); err != nil {
			return nil, nil, err
		}
		c.SnippetLibraries[key] = l
	}
	written := make(map[string]json.RawMessage, len(doc.Flows))
	for key, node := range doc.Flows {
		var v any
		if err := node.Decode(&v); err != nil {
			return nil, nil, fmt.Errorf("config: flows.%s: %w", key, err)
		}
		js, err := json.Marshal(v)
		if err != nil {
			return nil, nil, fmt.Errorf("config: flows.%s: not JSON-compatible: %w", key, err)
		}
		var f flowdef.Flow
		if err := json.Unmarshal(js, &f); err != nil {
			return nil, nil, fmt.Errorf("config: flows.%s: %w", key, err)
		}
		switch f.ID {
		case "":
			f.ID = key
		case key:
		default:
			return nil, nil, fmt.Errorf("config: flows.%s: id %q must match the key", key, f.ID)
		}
		c.Flows[key] = f
		written[key] = js
	}
	return c, written, nil
}

// keyed sets an artifact's id or name to its key, or checks that they match.
func keyed(a interface{ Key() (*string, string) }, section, key string) error {
	ref, field := a.Key()
	switch *ref {
	case "":
		*ref = key
	case key:
	default:
		return fmt.Errorf("config: %s.%s: %s %q must match the key", section, key, field, *ref)
	}
	return nil
}

func normalize(c *Config) {
	if c.Flows == nil {
		c.Flows = map[string]flowdef.Flow{}
	}
	if c.Alerts == nil {
		c.Alerts = map[string]artifact.Alert{}
	}
	if c.Snippets == nil {
		c.Snippets = map[string]artifact.Snippet{}
	}
	if c.SnippetLibraries == nil {
		c.SnippetLibraries = map[string]artifact.SnippetLibrary{}
	}
	if c.Scripts == nil {
		c.Scripts = map[string]string{}
	}
	if c.ConfigMap == nil {
		c.ConfigMap = map[string]string{}
	}
	if c.Settings == nil {
		c.Settings = map[string]any{}
	}
}

// Marshal serializes the config as YAML; flows are written as their JSON
// documents.
func (c *Config) Marshal() ([]byte, error) {
	flows := make(map[string]any, len(c.Flows))
	for k, f := range c.Flows {
		js, err := json.Marshal(f)
		if err != nil {
			return nil, err
		}
		var v any
		if err := json.Unmarshal(js, &v); err != nil {
			return nil, err
		}
		flows[k] = v
	}
	return yaml.Marshal(struct {
		*Config `yaml:",inline"`
		Flows   map[string]any `yaml:"flows"`
	}{c, flows})
}

// Artifacts flattens the config into artifact keys -> serialized content.
// Keys are "<kind>/<name>": flow, alert, snippet, library, script,
// configmap, settings.
func (c *Config) Artifacts() map[string][]byte {
	out := make(map[string][]byte)
	for k, v := range c.Flows {
		out["flow/"+k] = mustJSON(v)
	}
	for k, v := range c.Alerts {
		out["alert/"+k] = mustJSON(v)
	}
	for k, v := range c.Snippets {
		out["snippet/"+k] = mustJSON(v)
	}
	for k, v := range c.SnippetLibraries {
		out["library/"+k] = mustJSON(v)
	}
	for k, v := range c.Scripts {
		out["script/"+k] = []byte(v)
	}
	for k, v := range c.ConfigMap {
		out["configmap/"+k] = []byte(v)
	}
	for k, v := range c.Settings {
		out["settings/"+k] = mustJSON(v)
	}
	return out
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// Store is the live-state port consumed by plan/apply/drift. It is satisfied
// by the State Manager via a composition-root adapter (arch §3.1).
type Store interface {
	List(ctx context.Context) (map[string][]byte, error)
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Put(ctx context.Context, key string, value []byte) error
	Delete(ctx context.Context, key string) error
}

// ConfigSource is the source-of-truth port (the Git-backed config store),
// satisfied by gitstore via a composition-root adapter (arch §6).
type ConfigSource interface {
	List() (map[string][]byte, error)
}

// AuditSink records applied plans so "what changed and who approved" is
// reconstructable (gap #6).
type AuditSink interface {
	Record(ctx context.Context, action string, detail map[string]any) error
}

// MemStore is an in-memory Store adapter (tests + local DX).
type MemStore struct {
	mu sync.Mutex
	m  map[string][]byte
}

// NewMemStore returns an empty in-memory store.
func NewMemStore() *MemStore { return &MemStore{m: make(map[string][]byte)} }

func (m *MemStore) List(context.Context) (map[string][]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string][]byte, len(m.m))
	for k, v := range m.m {
		out[k] = v
	}
	return out, nil
}

func (m *MemStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.m[key]
	return v, ok, nil
}

func (m *MemStore) Put(_ context.Context, key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.m[key] = value
	return nil
}

func (m *MemStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.m, key)
	return nil
}

// MemSource is an in-memory Source adapter (tests).
type MemSource struct {
	m map[string][]byte
}

// NewMemSource returns a MemSource over the given artifacts.
func NewMemSource(m map[string][]byte) *MemSource {
	return &MemSource{m: m}
}

func (m *MemSource) List() (map[string][]byte, error) { return m.m, nil }

var (
	_ Store        = (*MemStore)(nil)
	_ ConfigSource = (*MemSource)(nil)
)
