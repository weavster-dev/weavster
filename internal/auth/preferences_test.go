package auth

import (
	"context"
	"errors"
	"testing"
)

// TestPreferences: preferences are replaced whole, saved with the account,
// kept by an account update, and copied out; unknown users and a failing
// store are reported.
func TestPreferences(t *testing.T) {
	ctx := context.Background()
	store := &memUsers{users: map[string]User{}}
	p := NewLocalProvider(Options{Store: store})
	if err := p.CreateUser(ctx, User{Username: "u", PasswordHash: "Pass-word-1"}); err != nil {
		t.Fatal(err)
	}
	if prefs, err := p.Preferences(ctx, "u"); err != nil || prefs == nil || len(prefs) != 0 {
		t.Errorf("new user's preferences = %v %v", prefs, err)
	}
	before := p.users["u"]
	if err := p.SetPreferences(ctx, "u", map[string]string{"theme": "dark"}); err != nil {
		t.Fatal(err)
	}
	if p.users["u"] != before { // an authentication in progress must not see a change
		t.Error("SetPreferences replaced the user record")
	}
	if store.users["u"].Preferences["theme"] != "dark" {
		t.Errorf("stored = %+v", store.users["u"])
	}
	if err := p.UpdateUser(ctx, "u", User{Email: "u@example.com"}); err != nil {
		t.Fatal(err)
	}
	prefs, _ := p.Preferences(ctx, "u")
	prefs["theme"] = "changed" // a copy
	if again, _ := p.Preferences(ctx, "u"); again["theme"] != "dark" {
		t.Errorf("after an update and a changed copy = %v", again)
	}
	if _, err := p.Preferences(ctx, "nobody"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("unknown user = %v", err)
	}
	if err := p.SetPreferences(ctx, "nobody", nil); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("unknown user = %v", err)
	}
	store.failAll = errors.New("disk full")
	if err := p.SetPreferences(ctx, "u", map[string]string{"theme": "light"}); !errors.Is(err, ErrStorage) {
		t.Errorf("failing store = %v", err)
	}
	if kept, _ := p.Preferences(ctx, "u"); kept["theme"] != "dark" {
		t.Errorf("a failed save changed the preferences: %v", kept)
	}
}

func TestCheckPassword(t *testing.T) {
	p := NewLocalProvider(Options{Policy: PasswordPolicy{MinLength: 8, MinUpper: 1}})
	if err := p.CheckPassword("Long-enough"); err != nil {
		t.Errorf("good password: %v", err)
	}
	if err := p.CheckPassword("short"); err == nil {
		t.Error("short password accepted")
	}
}
