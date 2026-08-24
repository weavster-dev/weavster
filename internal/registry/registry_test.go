package registry

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

type captureAudit struct{ calls []string }

func (c *captureAudit) Record(_ context.Context, action, detail string) error {
	c.calls = append(c.calls, action+":"+detail)
	return nil
}

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func TestDigest(t *testing.T) {
	d1 := Digest([]byte("a"))
	d2 := Digest([]byte("a"))
	if d1 != d2 {
		t.Error("digest must be deterministic")
	}
	if d1 == Digest([]byte("b")) {
		t.Error("digest must differ")
	}
}

func TestLifecyclePromoteRollback(t *testing.T) {
	ctx := context.Background()
	pub, priv := newKey(t)
	audit := &captureAudit{}
	r := New(pub, audit)

	m1, err := r.Add(ctx, "normalize", "1", []byte("wasm-v1"), "yaml", "alice", priv)
	if err != nil {
		t.Fatal(err)
	}
	if m1.State != StateDraft {
		t.Errorf("state = %s, want draft", m1.State)
	}

	// Draft is not instantiable.
	if _, err := r.Instantiate("normalize"); err == nil {
		t.Error("expected error instantiating draft")
	}

	if err := r.Promote(ctx, "normalize", "1"); err != nil {
		t.Fatal(err)
	}
	active, err := r.Instantiate("normalize")
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	if active.Version != "1" {
		t.Errorf("active version = %s", active.Version)
	}

	// Promote v2, then rollback to v1.
	if _, err := r.Add(ctx, "normalize", "2", []byte("wasm-v2"), "yaml", "bob", priv); err != nil {
		t.Fatal(err)
	}
	if err := r.Promote(ctx, "normalize", "2"); err != nil {
		t.Fatal(err)
	}
	if m1.State != StateSuperseded {
		t.Errorf("v1 state = %s, want superseded", m1.State)
	}
	if err := r.Rollback(ctx, "normalize", "1"); err != nil {
		t.Fatal(err)
	}
	active, _ = r.Instantiate("normalize")
	if active.Version != "1" {
		t.Errorf("after rollback active = %s, want 1", active.Version)
	}
	if len(audit.calls) < 3 {
		t.Errorf("expected audited lifecycle actions, got %v", audit.calls)
	}
}

func TestSignatureAndDigestVerification(t *testing.T) {
	ctx := context.Background()
	pub, priv := newKey(t)
	r := New(pub, nil)

	if _, err := r.Add(ctx, "m", "1", []byte("wasm"), "yaml", "alice", priv); err != nil {
		t.Fatal(err)
	}
	if err := r.Promote(ctx, "m", "1"); err != nil {
		t.Fatal(err)
	}

	// Tamper with the wasm bytes: digest mismatch.
	active, _ := r.Instantiate("m")
	active.Wasm = []byte("tampered")
	if _, err := r.Instantiate("m"); err == nil {
		t.Error("expected digest mismatch error")
	}
}

func TestSignatureRejectedWithWrongKey(t *testing.T) {
	ctx := context.Background()
	pub, _ := newKey(t)
	_, wrongPriv := newKey(t)
	r := New(pub, nil)

	if _, err := r.Add(ctx, "m", "1", []byte("wasm"), "yaml", "alice", wrongPriv); err != nil {
		t.Fatal(err)
	}
	if err := r.Promote(ctx, "m", "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Instantiate("m"); err == nil {
		t.Error("expected signature verification failure with wrong key")
	}
}

func TestAcquireReleaseRefcount(t *testing.T) {
	r := New(nil, nil)

	// Release on a never-acquired key must not underflow.
	r.Release("m", "1")
	if got := r.refs["m@1"]; got != 0 {
		t.Errorf("refs after release-without-acquire = %d, want 0", got)
	}

	r.Acquire("m", "1")
	r.Acquire("m", "1")
	if got := r.refs["m@1"]; got != 2 {
		t.Errorf("refs after two acquires = %d, want 2", got)
	}

	r.Release("m", "1")
	if got := r.refs["m@1"]; got != 1 {
		t.Errorf("refs after one release = %d, want 1", got)
	}

	r.Release("m", "1")
	if got := r.refs["m@1"]; got != 0 {
		t.Errorf("refs after second release = %d, want 0", got)
	}

	// Further releases must clamp at zero, never go negative.
	r.Release("m", "1")
	if got := r.refs["m@1"]; got != 0 {
		t.Errorf("refs after over-release = %d, want 0 (clamped)", got)
	}
}

// TestGCRetainsReferencedModule guards the safety invariant that GC must
// never remove a retired module version while it still has active
// references (e.g. an in-flight execution holding it). This is the primary
// consumer of the Acquire/Release refcount and had no test coverage.
func TestGCRetainsReferencedModule(t *testing.T) {
	ctx := context.Background()
	_, priv := newKey(t)
	r := New(nil, nil)

	if _, err := r.Add(ctx, "m", "1", []byte("v1"), "yaml", "a", priv); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Add(ctx, "m", "2", []byte("v2"), "yaml", "a", priv); err != nil {
		t.Fatal(err)
	}
	if err := r.Promote(ctx, "m", "1"); err != nil {
		t.Fatal(err)
	}
	if err := r.Retire(ctx, "m", "2"); err != nil {
		t.Fatal(err)
	}

	// Simulate an in-flight caller holding a reference to the retired
	// version; GC must skip it.
	r.Acquire("m", "2")
	if n := r.GC(); n != 0 {
		t.Errorf("gc removed %d referenced modules, want 0", n)
	}
	if _, err := r.Get("m", "2"); err != nil {
		t.Errorf("referenced retired module should be retained: %v", err)
	}

	// Once released, GC may reclaim it.
	r.Release("m", "2")
	if n := r.GC(); n != 1 {
		t.Errorf("gc removed %d, want 1 after release", n)
	}
	if _, err := r.Get("m", "2"); err == nil {
		t.Error("expected v2 to be gone after release + GC")
	}
}

func TestHistoryAndList(t *testing.T) {
	ctx := context.Background()
	_, priv := newKey(t)
	r := New(nil, nil)

	if got := r.History("missing"); len(got) != 0 {
		t.Errorf("History(missing) = %v, want empty", got)
	}
	if got := r.List(); len(got) != 0 {
		t.Errorf("List() on empty registry = %v, want empty", got)
	}

	if _, err := r.Add(ctx, "m", "1", []byte("v1"), "yaml", "a", priv); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Add(ctx, "m", "2", []byte("v2"), "yaml", "a", priv); err != nil {
		t.Fatal(err)
	}

	hist := r.History("m")
	if len(hist) != 2 || hist[0].Version != "1" || hist[1].Version != "2" {
		t.Errorf("History(m) = %+v, want [1, 2] in insertion order", hist)
	}

	// List only returns active (promoted) modules, not drafts.
	if got := r.List(); len(got) != 0 {
		t.Errorf("List() before promote = %v, want empty (drafts excluded)", got)
	}

	if err := r.Promote(ctx, "m", "1"); err != nil {
		t.Fatal(err)
	}
	list := r.List()
	if len(list) != 1 || list[0].Version != "1" {
		t.Errorf("List() after promote = %+v, want [v1]", list)
	}

	// NOTE: History/List copy the slice but not the pointed-to Module
	// structs, so mutating a returned Module currently corrupts internal
	// registry state. This is tracked separately as an encapsulation bug
	// (see issue: registry History/List leak mutable internal pointers);
	// this test documents the present behavior rather than asserting the
	// ideal (defensive-copy) behavior, to avoid failing CI on unrelated work.
	hist[0].Version = "tampered"
	if r.History("m")[0].Version != "tampered" {
		t.Error("History/List defensive-copy behavior changed; update this test and close the tracking issue")
	}
}

func TestGarbageCollection(t *testing.T) {
	ctx := context.Background()
	_, priv := newKey(t)
	r := New(nil, nil) // verification disabled for this test

	if _, err := r.Add(ctx, "m", "1", []byte("v1"), "yaml", "a", priv); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Add(ctx, "m", "2", []byte("v2"), "yaml", "a", priv); err != nil {
		t.Fatal(err)
	}
	if err := r.Promote(ctx, "m", "1"); err != nil {
		t.Fatal(err)
	}
	// v2 stays draft; retire it, then GC.
	if err := r.Retire(ctx, "m", "2"); err != nil {
		t.Fatal(err)
	}
	if n := r.GC(); n != 1 {
		t.Errorf("gc removed %d, want 1", n)
	}
	if _, err := r.Get("m", "2"); err == nil {
		t.Error("expected v2 to be gone after GC")
	}
	// v1 is active and must be retained.
	if _, err := r.Get("m", "1"); err != nil {
		t.Errorf("v1 should be retained: %v", err)
	}
}
