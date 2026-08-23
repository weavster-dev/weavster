// Package config implements config-as-code: JSON-Schema validation, plan,
// apply, and drift detection.
package config

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"gopkg.in/yaml.v3"
)

// Config is the root config-as-code document: the single source of truth for
// flows, alerts, snippets, scripts, the config map, and settings (arch §6).
type Config struct {
	Version  string            `json:"version" yaml:"version"`
	Flows    map[string]Flow   `json:"flows" yaml:"flows"`
	Alerts   map[string]Alert  `json:"alerts" yaml:"alerts"`
	Snippets map[string]string `json:"snippets" yaml:"snippets"`
	Scripts  map[string]string `json:"scripts" yaml:"scripts"`
	Map      map[string]string `json:"map" yaml:"map"`
	Settings map[string]any    `json:"settings" yaml:"settings"`
}

// Flow is a message pipeline (source -> filters/transforms -> destinations).
type Flow struct {
	Name         string        `json:"name" yaml:"name"`
	Source       Source        `json:"source" yaml:"source"`
	Destinations []Destination `json:"destinations" yaml:"destinations"`
	Filters      []Transform   `json:"filters,omitempty" yaml:"filters,omitempty"`
	Transforms   []Transform   `json:"transforms,omitempty" yaml:"transforms,omitempty"`
}

// Source selects a message acquisition adapter.
type Source struct {
	Type   string         `json:"type" yaml:"type"`
	Config map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
}

// Destination selects a message delivery adapter.
type Destination struct {
	Name   string         `json:"name" yaml:"name"`
	Type   string         `json:"type" yaml:"type"`
	Config map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
}

// Transform is a declarative filter/transform step (map/build/filter).
type Transform struct {
	Kind string         `json:"kind" yaml:"kind"`
	Spec map[string]any `json:"spec" yaml:"spec"`
}

// Alert is an alert definition (spec §2.7.24).
type Alert struct {
	Trigger    string   `json:"trigger" yaml:"trigger"`
	Recipients []string `json:"recipients" yaml:"recipients"`
	Scope      string   `json:"scope" yaml:"scope"`
	Enabled    bool     `json:"enabled" yaml:"enabled"`
}

// Parse decodes a config document. YAML is the canonical format; JSON is a
// YAML subset and is accepted as-is (arch §6).
func Parse(data []byte) (*Config, error) {
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("config: parse: %w", err)
	}
	if c.Version == "" {
		c.Version = "1"
	}
	normalize(&c)
	return &c, nil
}

func normalize(c *Config) {
	if c.Flows == nil {
		c.Flows = map[string]Flow{}
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

// Marshal serializes the config as YAML.
func (c *Config) Marshal() ([]byte, error) {
	return yaml.Marshal(c)
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
