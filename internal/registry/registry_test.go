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
