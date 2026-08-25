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

func TestAcquireRelease(t *testing.T) {
	ctx := context.Background()
	_, priv := newKey(t)
	r := New(nil, nil)

	if _, err := r.Add(ctx, "m", "1", []byte("wasm"), "yaml", "alice", priv); err != nil {
		t.Fatal(err)
	}

	r.Acquire("m", "1")
	r.Acquire("m", "1")
	r.Release("m", "1")
	r.Release("m", "1")
	// Extra release must not go negative (no panic).
	r.Release("m", "1")
}

func TestHistoryAndList(t *testing.T) {
	ctx := context.Background()
	_, priv := newKey(t)
	r := New(nil, nil)

	if _, err := r.Add(ctx, "m", "1", []byte("v1"), "yaml", "alice", priv); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Add(ctx, "m", "2", []byte("v2"), "yaml", "alice", priv); err != nil {
		t.Fatal(err)
	}
	if err := r.Promote(ctx, "m", "1"); err != nil {
		t.Fatal(err)
	}

	history := r.History("m")
	if len(history) != 2 {
		t.Fatalf("history len = %d, want 2", len(history))
	}
	if history[0].Version != "1" || history[1].Version != "2" {
		t.Errorf("history versions = %s, %s", history[0].Version, history[1].Version)
	}

	list := r.List()
	if len(list) != 1 || list[0].Version != "1" {
		t.Errorf("list = %+v", list)
	}

	// History for unknown name returns empty slice.
	if h := r.History("unknown"); len(h) != 0 {
		t.Errorf("unknown history = %v", h)
	}
}

func TestAddDuplicateVersionError(t *testing.T) {
	ctx := context.Background()
	_, priv := newKey(t)
	r := New(nil, nil)

	if _, err := r.Add(ctx, "m", "1", []byte("wasm"), "yaml", "alice", priv); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Add(ctx, "m", "1", []byte("wasm"), "yaml", "alice", priv); err == nil {
		t.Error("expected error adding duplicate version")
	}
}

func TestPromoteNotFoundError(t *testing.T) {
	r := New(nil, nil)
	if err := r.Promote(context.Background(), "noexist", "1"); err == nil {
		t.Error("expected error promoting non-existent module")
	}
}

func TestRollbackSameVersion(t *testing.T) {
	ctx := context.Background()
	_, priv := newKey(t)
	r := New(nil, nil)

	if _, err := r.Add(ctx, "m", "1", []byte("v1"), "yaml", "alice", priv); err != nil {
		t.Fatal(err)
	}
	if err := r.Promote(ctx, "m", "1"); err != nil {
		t.Fatal(err)
	}
	// Rollback to same version should succeed without error.
	if err := r.Rollback(ctx, "m", "1"); err != nil {
		t.Fatalf("rollback same version: %v", err)
	}
}

func TestRollbackNotFoundError(t *testing.T) {
	r := New(nil, nil)
	if err := r.Rollback(context.Background(), "noexist", "1"); err == nil {
		t.Error("expected error rolling back non-existent module")
	}
}

func TestRetireActiveError(t *testing.T) {
	ctx := context.Background()
	_, priv := newKey(t)
	r := New(nil, nil)

	if _, err := r.Add(ctx, "m", "1", []byte("v1"), "yaml", "alice", priv); err != nil {
		t.Fatal(err)
	}
	if err := r.Promote(ctx, "m", "1"); err != nil {
		t.Fatal(err)
	}
	if err := r.Retire(ctx, "m", "1"); err == nil {
		t.Error("expected error retiring active module")
	}
}

func TestRetireNotFoundError(t *testing.T) {
	r := New(nil, nil)
	if err := r.Retire(context.Background(), "noexist", "1"); err == nil {
		t.Error("expected error retiring non-existent module")
	}
}

func TestPromoteAlreadyActiveNoOp(t *testing.T) {
	ctx := context.Background()
	_, priv := newKey(t)
	r := New(nil, nil)

	if _, err := r.Add(ctx, "m", "1", []byte("v1"), "yaml", "alice", priv); err != nil {
		t.Fatal(err)
	}
	if err := r.Promote(ctx, "m", "1"); err != nil {
		t.Fatal(err)
	}
	// Promoting an already-active module should be a no-op.
	if err := r.Promote(ctx, "m", "1"); err != nil {
		t.Fatalf("re-promote: %v", err)
	}
}

func TestInstantiateNoActive(t *testing.T) {
	r := New(nil, nil)
	if _, err := r.Instantiate("noexist"); err == nil {
		t.Error("expected error for no active module")
	}
}

func TestGetModule(t *testing.T) {
	ctx := context.Background()
	_, priv := newKey(t)
	r := New(nil, nil)

	if _, err := r.Add(ctx, "m", "1", []byte("v1"), "yaml", "alice", priv); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get("m", "1"); err != nil {
		t.Fatalf("get: %v", err)
	}
	if _, err := r.Get("m", "99"); err == nil {
		t.Error("expected error for unknown version")
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
