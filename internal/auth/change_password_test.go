package auth

import (
	"context"
	"errors"
	"testing"
)

func TestChangePasswordGuards(t *testing.T) {
	ctx := context.Background()
	p := NewLocalProvider(Options{Lockout: LockoutPolicy{RetryLimit: 2, LockoutPeriod: 60}})
	if err := p.CreateUser(ctx, User{Username: "u", PasswordHash: "Old-Pass-1", MustChangePassword: true}); err != nil {
		t.Fatal(err)
	}
	if err := p.ChangePassword(ctx, "u", "Old-Pass-1", "Old-Pass-1"); !errors.Is(err, ErrPasswordReused) {
		t.Errorf("same password: err = %v, want ErrPasswordReused", err)
	}
	for i := 0; i < 2; i++ {
		if err := p.ChangePassword(ctx, "u", "wrong", "New-Pass-2"); !errors.Is(err, ErrPasswordWrong) {
			t.Errorf("wrong old: err = %v, want ErrPasswordWrong", err)
		}
	}
	if _, err := p.Authenticate(ctx, "u", "Old-Pass-1", ""); err == nil {
		t.Error("wrong old passwords did not count toward lockout")
	}
}

func TestAuthenticateReturnsCopy(t *testing.T) {
	ctx := context.Background()
	p := NewLocalProvider(Options{})
	if err := p.CreateUser(ctx, User{Username: "u", PasswordHash: "Pass-1", MustChangePassword: true}); err != nil {
		t.Fatal(err)
	}
	u, err := p.Authenticate(ctx, "u", "Pass-1", "")
	if err != nil {
		t.Fatal(err)
	}
	u.MustChangePassword = false
	if stored, _ := p.GetUser(ctx, "u"); !stored.MustChangePassword {
		t.Error("mutating the returned user changed the stored record")
	}
}
