package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

type memUsers struct {
	users   map[string]User
	failAll error
}

func (m *memUsers) LoadUsers(context.Context) ([]User, error) {
	out := []User{}
	for _, u := range m.users {
		out = append(out, u)
	}
	return out, m.failAll
}

func (m *memUsers) SaveUser(_ context.Context, u User) error {
	if m.failAll != nil {
		return m.failAll
	}
	m.users[u.Username] = u
	return nil
}

func (m *memUsers) DeleteUser(_ context.Context, username string) error {
	if m.failAll != nil {
		return m.failAll
	}
	delete(m.users, username)
	return nil
}

func TestLocalProviderPersistsUsers(t *testing.T) {
	ctx := context.Background()
	store := &memUsers{users: map[string]User{}}
	p := NewLocalProvider(Options{Store: store, Lockout: LockoutPolicy{RetryLimit: 3, LockoutPeriod: 60}})
	if err := p.CreateUser(ctx, User{Username: "u", PasswordHash: "Pass-1"}); err != nil {
		t.Fatal(err)
	}
	if err := p.UpdateUser(ctx, "u", User{Username: "u", Permissions: []string{"flows:view"}}); err != nil {
		t.Fatal(err)
	}
	if err := p.ChangePassword(ctx, "u", "Pass-1", "Pass-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Authenticate(ctx, "u", "wrong", ""); err == nil {
		t.Fatal("wrong password authenticated")
	}
	if store.users["u"].FailedAttempts != 1 {
		t.Errorf("failed attempt not persisted: %+v", store.users["u"])
	}
	if _, err := p.Authenticate(ctx, "u", "Pass-2", ""); err != nil {
		t.Fatal(err)
	}
	if store.users["u"].FailedAttempts != 0 {
		t.Error("successful login did not persist the reset counter")
	}

	// A fresh provider loads the persisted state.
	q := NewLocalProvider(Options{Store: store})
	if err := q.Load(ctx); err != nil {
		t.Fatal(err)
	}
	u, err := q.Authenticate(ctx, "u", "Pass-2", "")
	if err != nil || len(u.Permissions) != 1 {
		t.Errorf("reloaded user = %+v, %v", u, err)
	}
	if err := q.DeleteUser(ctx, "u"); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.users["u"]; ok {
		t.Error("delete not persisted")
	}
}

func TestLocalProviderSaveFailureLeavesStateUnchanged(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("disk full")
	store := &memUsers{users: map[string]User{}}
	p := NewLocalProvider(Options{Store: store})
	if err := p.CreateUser(ctx, User{Username: "u", PasswordHash: "Pass-1"}); err != nil {
		t.Fatal(err)
	}
	store.failAll = boom

	if err := p.CreateUser(ctx, User{Username: "v", PasswordHash: "Pass-1"}); !errors.Is(err, boom) {
		t.Errorf("CreateUser = %v, want save error", err)
	}
	if _, err := p.GetUser(ctx, "v"); err == nil {
		t.Error("user created in memory despite save failure")
	}
	if err := p.UpdateUser(ctx, "u", User{Username: "u", Permissions: []string{"admin"}}); !errors.Is(err, boom) {
		t.Errorf("UpdateUser = %v, want save error", err)
	}
	if err := p.ChangePassword(ctx, "u", "Pass-1", "Pass-2"); !errors.Is(err, boom) {
		t.Errorf("ChangePassword = %v, want save error", err)
	}
	if err := p.DeleteUser(ctx, "u"); !errors.Is(err, boom) {
		t.Errorf("DeleteUser = %v, want save error", err)
	}
	u, err := p.Authenticate(ctx, "u", "Pass-1", "")
	if err != nil || len(u.Permissions) != 0 {
		t.Errorf("state changed despite save failures: %+v, %v", u, err)
	}
	if err := p.Load(ctx); !errors.Is(err, boom) {
		t.Errorf("Load = %v, want load error", err)
	}
	if err := NewLocalProvider(Options{}).Load(ctx); err != nil {
		t.Errorf("Load without store = %v", err)
	}
}

func TestUpdateUserPreservesSecurityState(t *testing.T) {
	ctx := context.Background()
	store := &memUsers{users: map[string]User{}}
	p := NewLocalProvider(Options{Store: store, Lockout: LockoutPolicy{RetryLimit: 1, LockoutPeriod: 60}})
	if err := p.CreateUser(ctx, User{Username: "admin", PasswordHash: "Pass-1", MustChangePassword: true}); err != nil {
		t.Fatal(err)
	}
	_, _ = p.Authenticate(ctx, "admin", "wrong", "") // locks
	if err := p.UpdateUser(ctx, "admin", User{Permissions: []string{"admin"}}); err != nil {
		t.Fatal(err)
	}
	saved := store.users["admin"]
	if saved.Username != "admin" || !saved.MustChangePassword || saved.LockedUntil.IsZero() || len(store.users) != 1 {
		t.Errorf("update changed security state or key: %+v (rows %d)", saved, len(store.users))
	}
}

func TestSaveCountersLogsFailure(t *testing.T) {
	ctx := context.Background()
	var logs strings.Builder
	store := &memUsers{users: map[string]User{}}
	p := NewLocalProvider(Options{Store: store, Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	if err := p.CreateUser(ctx, User{Username: "u", PasswordHash: "Pass-1"}); err != nil {
		t.Fatal(err)
	}
	store.failAll = errors.New("disk full")
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, _ = p.Authenticate(cancelled, "u", "wrong", "")
	if !strings.Contains(logs.String(), "lockout state not persisted") || !strings.Contains(logs.String(), "disk full") {
		t.Errorf("logs = %q", logs.String())
	}
	if u, _ := p.GetUser(ctx, "u"); u.FailedAttempts != 1 {
		t.Error("in-memory strike lost")
	}
}
