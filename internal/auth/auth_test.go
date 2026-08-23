package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeMFA struct{ err error }

func (m *fakeMFA) Verify(context.Context, *User, string) error { return m.err }

type fakeExternal struct{ ok bool }

func (e *fakeExternal) Authenticate(context.Context, string, string) (bool, error) { return e.ok, nil }

func testOptions() Options {
	return Options{
		Policy:          PasswordPolicy{MinLength: 8, MinUpper: 1, MinLower: 1, MinNumeric: 1},
		Lockout:         LockoutPolicy{RetryLimit: 3, LockoutPeriod: 60},
		AntiEnumeration: true,
	}
}

func TestUserCRUDAndAuthenticate(t *testing.T) {
	p := NewLocalProvider(testOptions())
	ctx := context.Background()

	if err := p.CreateUser(ctx, User{Username: "alice", PasswordHash: "Passw0rd!", Permissions: []string{PermFlowsView}}); err != nil {
		t.Fatal(err)
	}
	if err := p.CreateUser(ctx, User{Username: "alice", PasswordHash: "x"}); err != ErrUserExists {
		t.Errorf("expected ErrUserExists, got %v", err)
	}

	u, err := p.Authenticate(ctx, "alice", "Passw0rd!", "")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if u.Username != "alice" {
		t.Errorf("user = %+v", u)
	}

	// Wrong password → generic message (anti-enumeration on).
	if _, err := p.Authenticate(ctx, "alice", "wrong", ""); err != ErrGenericFailure {
		t.Errorf("expected generic failure, got %v", err)
	}

	// Change password.
	if err := p.ChangePassword(ctx, "alice", "Passw0rd!", "NewPass1!"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Authenticate(ctx, "alice", "NewPass1!", ""); err != nil {
		t.Errorf("new password auth: %v", err)
	}

	// Delete.
	if err := p.DeleteUser(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.GetUser(ctx, "alice"); err != ErrUserNotFound {
		t.Errorf("expected ErrUserNotFound, got %v", err)
	}
}

func TestPasswordPolicy(t *testing.T) {
	pol := PasswordPolicy{MinLength: 8, MinUpper: 1, MinLower: 1, MinNumeric: 1, MinSpecial: -1}
	if err := pol.Validate("Passw0rd!"); err == nil {
		t.Error("special characters must be forbidden when MinSpecial=-1")
	}
	if err := pol.Validate("Passw0rd"); err != nil {
		t.Errorf("valid password rejected: %v", err)
	}
	if err := pol.Validate("short1A"); err == nil {
		t.Error("short password must be rejected")
	}
}

func TestLockoutAndDecay(t *testing.T) {
	p := NewLocalProvider(Options{
		Policy:          PasswordPolicy{MinLength: 4},
		Lockout:         LockoutPolicy{RetryLimit: 2, LockoutPeriod: 60},
		AntiEnumeration: true,
	})
	ctx := context.Background()
	_ = p.CreateUser(ctx, User{Username: "bob", PasswordHash: "pass"})

	for i := 0; i < 2; i++ {
		if _, err := p.Authenticate(ctx, "bob", "wrong", ""); err != ErrGenericFailure {
			t.Fatalf("failure %d: %v", i, err)
		}
	}
	// Locked: even the correct password fails with the generic message.
	if _, err := p.Authenticate(ctx, "bob", "pass", ""); err != ErrGenericFailure {
		t.Errorf("expected lockout generic failure, got %v", err)
	}

	// Force lockout expiry and verify strike decay allows login again.
	p.mu.Lock()
	p.users["bob"].LockedUntil = time.Now().Add(-time.Minute)
	p.mu.Unlock()
	if _, err := p.Authenticate(ctx, "bob", "pass", ""); err != nil {
		t.Errorf("after lockout decay: %v", err)
	}
}

func TestMFAAndExternalHooks(t *testing.T) {
	// MFA hook runs after primary authN.
	p := NewLocalProvider(Options{
		Policy:          PasswordPolicy{MinLength: 4},
		Lockout:         LockoutPolicy{},
		AntiEnumeration: false,
		MFA:             &fakeMFA{err: errors.New("bad code")},
	})
	ctx := context.Background()
	_ = p.CreateUser(ctx, User{Username: "carol", PasswordHash: "pass"})
	if _, err := p.Authenticate(ctx, "carol", "pass", "000000"); err == nil {
		t.Error("expected MFA failure")
	}
	p.opts.MFA = &fakeMFA{err: nil}
	if _, err := p.Authenticate(ctx, "carol", "pass", "123456"); err != nil {
		t.Errorf("expected MFA success, got %v", err)
	}

	// External hook overrides built-in validation.
	ext := NewLocalProvider(Options{Policy: PasswordPolicy{}, External: &fakeExternal{ok: true}})
	_ = ext.CreateUser(ctx, User{Username: "dave", PasswordHash: "ignored"})
	if _, err := ext.Authenticate(ctx, "dave", "anything", ""); err != nil {
		t.Errorf("external hook should override: %v", err)
	}
}

func TestAuthorizer(t *testing.T) {
	az := NewLocalAuthorizer()
	ctx := context.Background()

	admin := &User{Permissions: []string{PermAdmin}}
	viewer := &User{Permissions: []string{PermFlowsView, PermMessagesView}}

	if !az.Authorize(ctx, admin, "flows", "edit") {
		t.Error("admin must be authorized")
	}
	if !az.Authorize(ctx, viewer, "flows", "view") {
		t.Error("viewer must view flows")
	}
	if az.Authorize(ctx, viewer, "flows", "edit") {
		t.Error("viewer must not edit flows")
	}
	if az.Authorize(ctx, nil, "flows", "view") {
		t.Error("nil user must be denied")
	}
}
