package registry

import (
	"context"
	"errors"
	"testing"
)

// errAuditBoom forces the audit sink to fail so lifecycle methods must
// propagate the audit error instead of swallowing it.
var errAuditBoom = errors.New("registry: audit sink failed")

type failAudit struct{}

func (failAudit) Record(_ context.Context, _, _ string) error { return errAuditBoom }

// TestAddDuplicateVersionRejected covers the "module already exists" guard in
// Add, which previously had no test and would silently overwrite the module
// map if it regressed to an upsert.
func TestAddDuplicateVersionRejected(t *testing.T) {
	ctx := context.Background()
	_, priv := newKey(t)
	r := New(nil, nil)

	if _, err := r.Add(ctx, "m", "1", []byte("v1"), "yaml", "a", priv); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Add(ctx, "m", "1", []byte("v1b"), "yaml", "b", priv); err == nil {
		t.Fatal("expected duplicate-version error")
	}
	// The original module must remain unchanged.
	hist := r.History("m")
	if len(hist) != 1 || string(hist[0].Wasm) != "v1" {
		t.Errorf("history after rejected duplicate = %+v, want single v1", hist)
	}
}

// TestPromoteNotFound covers the promote-missing-version error branch.
func TestPromoteNotFound(t *testing.T) {
	ctx := context.Background()
	r := New(nil, nil)
	if err := r.Promote(ctx, "missing", "1"); err == nil {
		t.Fatal("expected not-found error")
	}
}

// TestPromoteAlreadyActiveIsNoop covers the promote-an-already-active-version
// no-op branch, ensuring it does not error or reset module state.
func TestPromoteAlreadyActiveIsNoop(t *testing.T) {
	ctx := context.Background()
	_, priv := newKey(t)
	r := New(nil, nil)

	if _, err := r.Add(ctx, "m", "1", []byte("v1"), "yaml", "a", priv); err != nil {
		t.Fatal(err)
	}
	if err := r.Promote(ctx, "m", "1"); err != nil {
		t.Fatal(err)
	}
	if err := r.Promote(ctx, "m", "1"); err != nil {
		t.Fatalf("re-promote active: %v", err)
	}
	active, err := r.Instantiate("m")
	if err != nil {
		t.Fatal(err)
	}
	if active.State != StateActive {
		t.Errorf("state = %s, want active", active.State)
	}
}

// TestRollbackNotFound covers the rollback-missing-version error branch.
func TestRollbackNotFound(t *testing.T) {
	ctx := context.Background()
	r := New(nil, nil)
	if err := r.Rollback(ctx, "missing", "1"); err == nil {
		t.Fatal("expected not-found error")
	}
}

// TestRollbackToCurrentActiveIsNoop covers the rollback-to-the-currently-active
// version no-op branch, ensuring it does not supersede the active module.
func TestRollbackToCurrentActiveIsNoop(t *testing.T) {
	ctx := context.Background()
	_, priv := newKey(t)
	r := New(nil, nil)

	if _, err := r.Add(ctx, "m", "1", []byte("v1"), "yaml", "a", priv); err != nil {
		t.Fatal(err)
	}
	if err := r.Promote(ctx, "m", "1"); err != nil {
		t.Fatal(err)
	}
	if err := r.Rollback(ctx, "m", "1"); err != nil {
		t.Fatalf("rollback to active: %v", err)
	}
	active, err := r.Instantiate("m")
	if err != nil {
		t.Fatal(err)
	}
	if active.Version != "1" || active.State != StateActive {
		t.Errorf("active = %s@%s, want 1@active", active.Version, active.State)
	}
}

// TestRetireNotFound covers the retire-missing-version error branch.
func TestRetireNotFound(t *testing.T) {
	ctx := context.Background()
	r := New(nil, nil)
	if err := r.Retire(ctx, "missing", "1"); err == nil {
		t.Fatal("expected not-found error")
	}
}

// TestRetireActiveFails covers the safety guard that refuses to retire the
// currently-active module, which would otherwise orphan live traffic.
func TestRetireActiveFails(t *testing.T) {
	ctx := context.Background()
	_, priv := newKey(t)
	r := New(nil, nil)

	if _, err := r.Add(ctx, "m", "1", []byte("v1"), "yaml", "a", priv); err != nil {
		t.Fatal(err)
	}
	if err := r.Promote(ctx, "m", "1"); err != nil {
		t.Fatal(err)
	}
	if err := r.Retire(ctx, "m", "1"); err == nil {
		t.Fatal("expected cannot-retire-active error")
	}
	// The module must still be present and active after the rejected retire.
	active, err := r.Instantiate("m")
	if err != nil {
		t.Fatalf("active module vanished after rejected retire: %v", err)
	}
	if active.State != StateActive {
		t.Errorf("state = %s, want active", active.State)
	}
}

// TestInstantiateWithNilPublicKeySkipsSignature covers the nil-public-key path
// in Verify, where signature verification is disabled for local DX. It also
// confirms digest-mismatch detection still applies when verification is off.
func TestInstantiateWithNilPublicKeySkipsSignature(t *testing.T) {
	ctx := context.Background()
	_, priv := newKey(t)
	r := New(nil, nil) // signature verification disabled

	if _, err := r.Add(ctx, "m", "1", []byte("v1"), "yaml", "a", priv); err != nil {
		t.Fatal(err)
	}
	if err := r.Promote(ctx, "m", "1"); err != nil {
		t.Fatal(err)
	}
	// With a nil public key, Instantiate must skip signature verification
	// and succeed.
	if _, err := r.Instantiate("m"); err != nil {
		t.Fatalf("instantiate with nil pub: %v", err)
	}

	// Digest mismatch must still be detected even when signature
	// verification is disabled.
	active, _ := r.Instantiate("m")
	active.Wasm = []byte("tampered")
	if _, err := r.Instantiate("m"); err == nil {
		t.Fatal("expected digest-mismatch error even with nil pub")
	}
}

// TestAuditErrorPropagates covers the error return from the audit sink through
// each lifecycle method (Add/Promote/Rollback/Retire). A failure to record the
// audited action must not be swallowed. Each case seeds an independent registry
// so the lifecycle state is predictable (direct field access is available
// because this test lives in the registry package).
func TestAuditErrorPropagates(t *testing.T) {
	ctx := context.Background()
	_, priv := newKey(t)

	t.Run("Add", func(t *testing.T) {
		r := New(nil, failAudit{})
		if _, err := r.Add(ctx, "m", "1", []byte("v1"), "yaml", "a", priv); !errors.Is(err, errAuditBoom) {
			t.Fatalf("Add audit error = %v, want %v", err, errAuditBoom)
		}
	})

	t.Run("Promote", func(t *testing.T) {
		r := New(nil, failAudit{})
		r.modules["m"] = append(r.modules["m"], &Module{Name: "m", Version: "1", State: StateDraft})
		if err := r.Promote(ctx, "m", "1"); !errors.Is(err, errAuditBoom) {
			t.Fatalf("Promote audit error = %v, want %v", err, errAuditBoom)
		}
	})

	t.Run("Rollback", func(t *testing.T) {
		r := New(nil, failAudit{})
		m1 := &Module{Name: "m", Version: "1", State: StateSuperseded}
		m2 := &Module{Name: "m", Version: "2", State: StateActive}
		r.modules["m"] = append(r.modules["m"], m1, m2)
		r.active["m"] = m2
		if err := r.Rollback(ctx, "m", "1"); !errors.Is(err, errAuditBoom) {
			t.Fatalf("Rollback audit error = %v, want %v", err, errAuditBoom)
		}
	})

	t.Run("Retire", func(t *testing.T) {
		r := New(nil, failAudit{})
		r.modules["m"] = append(r.modules["m"], &Module{Name: "m", Version: "1", State: StateDraft})
		if err := r.Retire(ctx, "m", "1"); !errors.Is(err, errAuditBoom) {
			t.Fatalf("Retire audit error = %v, want %v", err, errAuditBoom)
		}
	})
}
