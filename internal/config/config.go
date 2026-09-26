// Package config implements config-as-code: JSON-Schema validation, plan,
// apply, and drift detection.
package config

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/weavster-dev/weavster/internal/flowdef"
)

// Config is the root config-as-code document: the single source of truth for
// flows, alerts, snippets, scripts, the config map, and settings (arch §6).
type Config struct {
	Version string `json:"version" yaml:"version"`
	// Flows are flow definitions keyed by flow id: the same model the flow
	// API accepts (flow.schema.json).
	Flows    map[string]flowdef.Flow `json:"flows" yaml:"-"` // decoded through JSON (parse)
	Alerts   map[string]Alert        `json:"alerts" yaml:"alerts"`
	Snippets map[string]string       `json:"snippets" yaml:"snippets"`
	Scripts  map[string]string       `json:"scripts" yaml:"scripts"`
	Map      map[string]string       `json:"map" yaml:"map"`
	Settings map[string]any          `json:"settings" yaml:"settings"`
}

// Alert is an alert definition (spec §2.7.24).
type Alert struct {
	Trigger    string   `json:"trigger" yaml:"trigger"`
	Recipients []string `json:"recipients" yaml:"recipients"`
	Scope      string   `json:"scope" yaml:"scope"`
	Enabled    bool     `json:"enabled" yaml:"enabled"`
}

// Parse decodes a config document. YAML is the canonical format; JSON is a
// YAML subset and is accepted as-is (arch §6). A flow's id defaults to its
// key and must match it when set.
func Parse(data []byte) (*Config, error) {
	c, _, err := parse(data)
	return c, err
}

// parse decodes the document with YAML rules, except flows: each flow is
// converted to JSON and decoded as a flow definition, so transforms stay
// JSON objects. It also returns each flow as written, for validation.
func parse(data []byte) (*Config, map[string]json.RawMessage, error) {
	var c Config
	var doc struct {
		Flows map[string]yaml.Node `yaml:"flows"`
	}
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, nil, fmt.Errorf("config: parse: %w", err)
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("config: parse: %w", err)
	}
	if c.Version == "" {
		c.Version = "1"
	}
	normalize(&c)
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
	return &c, written, nil
}

func normalize(c *Config) {
	if c.Flows == nil {
		c.Flows = map[string]flowdef.Flow{}
	}
	if c.Alerts == nil {
		c.Alerts = map[string]Alert{}
	}
	if c.Snippets == nil {
		c.Snippets = map[string]string{}
	}
	if c.Scripts == nil {
		c.Scripts = map[string]string{}
	}
	if c.Map == nil {
		c.Map = map[string]string{}
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
// Keys are of the form "<kind>/<name>" (e.g. "flow/myflow", "alert/myalert").
func (c *Config) Artifacts() map[string][]byte {
	out := make(map[string][]byte)
	for k, v := range c.Flows {
		out["flow/"+k] = mustJSON(v)
	}
	for k, v := range c.Alerts {
		out["alert/"+k] = mustJSON(v)
	}
	for k, v := range c.Snippets {
		out["snippet/"+k] = []byte(v)
	}
	for k, v := range c.Scripts {
		out["script/"+k] = []byte(v)
	}
	for k, v := range c.Map {
		out["map/"+k] = []byte(v)
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
