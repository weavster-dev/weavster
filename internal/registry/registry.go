// Package registry implements the content-addressed, signed WASM module
// registry with lifecycle, rollback, and garbage collection.
package registry

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
)

// Digest returns the SHA-256 content address of a WASM module (gap #2).
func Digest(wasm []byte) string {
	sum := sha256.Sum256(wasm)
	return hex.EncodeToString(sum[:])
}

// Module is one versioned WASM module (gap #2).
type Module struct {
	Name      string
	Version   string
	Digest    string
	Source    string
	Signature []byte
	CreatedBy string
	State     State
	Wasm      []byte
}

func (m *Module) key() string { return m.Name + "@" + m.Version }

// AuditSink records lifecycle actions (consumer-defined port).
type AuditSink interface {
	Record(ctx context.Context, action, detail string) error
}

// Registry is a content-addressed, signed WASM module registry (gap #2).
type Registry struct {
	mu      sync.Mutex
	modules map[string][]*Module // name -> versions (insertion order)
	active  map[string]*Module   // name -> active version
	refs    map[string]int       // name@version -> reference count
	pub     ed25519.PublicKey
	audit   AuditSink
}

// New returns a registry that verifies signatures with pub. A nil pub
// disables signature verification (local DX).
func New(pub ed25519.PublicKey, audit AuditSink) *Registry {
	return &Registry{
		modules: make(map[string][]*Module),
		active:  make(map[string]*Module),
		refs:    make(map[string]int),
		pub:     pub,
		audit:   audit,
	}
}

// Add stores a module as draft, signing its digest (gap #2).
func (r *Registry) Add(ctx context.Context, name, version string, wasm []byte, source, createdBy string, signKey ed25519.PrivateKey) (*Module, error) {
	digest := Digest(wasm)
	m := &Module{
		Name:      name,
		Version:   version,
		Digest:    digest,
		Source:    source,
		Signature: Sign(signKey, digest),
		CreatedBy: createdBy,
		State:     StateDraft,
		Wasm:      wasm,
	}
	r.mu.Lock()
	for _, existing := range r.modules[name] {
		if existing.Version == version {
			r.mu.Unlock()
			return nil, fmt.Errorf("registry: module %s@%s already exists", name, version)
		}
	}
	r.modules[name] = append(r.modules[name], m)
	r.mu.Unlock()
	return m, r.auditRecord(ctx, "module.add", m.key())
}

// Promote advances a module to active, superseding the current active version
// (an explicit, audited action; gap #2).
func (r *Registry) Promote(ctx context.Context, name, version string) error {
	r.mu.Lock()
	m := r.find(name, version)
	if m == nil {
		r.mu.Unlock()
		return fmt.Errorf("registry: module %s@%s not found", name, version)
	}
	if m.State == StateActive {
		r.mu.Unlock()
		return nil
	}
	if cur := r.active[name]; cur != nil {
		cur.State = StateSuperseded
	}
	m.State = StateActive
	r.active[name] = m
	r.mu.Unlock()
	return r.auditRecord(ctx, "module.promote", m.key())
}

// Rollback re-promotes a prior version to active (atomic pointer move, no
// rebuild; gap #2).
func (r *Registry) Rollback(ctx context.Context, name, version string) error {
	r.mu.Lock()
	m := r.find(name, version)
	if m == nil {
		r.mu.Unlock()
		return fmt.Errorf("registry: module %s@%s not found", name, version)
	}
	if cur := r.active[name]; cur != nil {
		if cur.Version == version {
			r.mu.Unlock()
			return nil
		}
		cur.State = StateSuperseded
	}
	m.State = StateActive
	r.active[name] = m
	r.mu.Unlock()
	return r.auditRecord(ctx, "module.rollback", m.key())
}

// Instantiate returns the active module after verifying its digest and
// signature (gap #2).
func (r *Registry) Instantiate(name string) (*Module, error) {
	r.mu.Lock()
	m := r.active[name]
	r.mu.Unlock()
	if m == nil {
		return nil, fmt.Errorf("registry: no active module for %s", name)
	}
	if Digest(m.Wasm) != m.Digest {
		return nil, fmt.Errorf("registry: digest mismatch for %s@%s", m.Name, m.Version)
	}
	if !Verify(r.pub, m.Digest, m.Signature) {
		return nil, fmt.Errorf("registry: signature verification failed for %s@%s", m.Name, m.Version)
	}
	return m, nil
}

// Retire marks a non-active module as retired (gap #2).
func (r *Registry) Retire(ctx context.Context, name, version string) error {
	r.mu.Lock()
	m := r.find(name, version)
	if m == nil {
		r.mu.Unlock()
		return fmt.Errorf("registry: module %s@%s not found", name, version)
	}
	if m.State == StateActive {
		r.mu.Unlock()
		return fmt.Errorf("registry: cannot retire active module %s@%s", name, version)
	}
	m.State = StateRetired
	r.mu.Unlock()
	return r.auditRecord(ctx, "module.retire", m.key())
}

// Acquire increments the reference count for a module version.
func (r *Registry) Acquire(name, version string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refs[name+"@"+version]++
}

// Release decrements the reference count for a module version.
func (r *Registry) Release(name, version string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := name + "@" + version
	if r.refs[k] > 0 {
		r.refs[k]--
	}
}

// Get returns a specific module version, or an error.
func (r *Registry) Get(name, version string) (*Module, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m := r.find(name, version); m != nil {
		return m, nil
	}
	return nil, fmt.Errorf("registry: module %s@%s not found", name, version)
}

// History returns all versions of a module in insertion order (newest last).
func (r *Registry) History(name string) []*Module {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Module, len(r.modules[name]))
	copy(out, r.modules[name])
	return out
}

// List returns the active modules.
func (r *Registry) List() []*Module {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Module, 0, len(r.active))
	for _, m := range r.active {
		out = append(out, m)
	}
	return out
}

func (r *Registry) find(name, version string) *Module {
	for _, m := range r.modules[name] {
		if m.Version == version {
			return m
		}
	}
	return nil
}

func (r *Registry) auditRecord(ctx context.Context, action, detail string) error {
	if r.audit == nil {
		return nil
	}
	return r.audit.Record(ctx, action, detail)
}
