package auth

import (
	"context"
	"testing"
	"time"
)

type expirationMFA struct{ calls int }

func (m *expirationMFA) Verify(context.Context, *User, string) error {
	m.calls++
	return nil
}

func TestAuthenticatePasswordExpiration(t *testing.T) {
	for _, tc := range []struct {
		name            string
		expiration      int
		age             time.Duration
		antiEnumeration bool
		wantExpired     bool
	}{
		{name: "expired", expiration: 30, age: 31 * 24 * time.Hour, wantExpired: true},
		{name: "expired with anti-enumeration", expiration: 30, age: 31 * 24 * time.Hour, antiEnumeration: true, wantExpired: true},
		{name: "fresh", expiration: 30, age: 24 * time.Hour},
		{name: "expiration disabled", age: 365 * 24 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			mfa := &expirationMFA{}
			p := NewLocalProvider(Options{
				Policy:          PasswordPolicy{Expiration: tc.expiration},
				AntiEnumeration: tc.antiEnumeration,
				MFA:             mfa,
			})
			if err := p.CreateUser(ctx, User{Username: "alice", PasswordHash: "Passw0rd!"}); err != nil {
				t.Fatal(err)
			}
			p.mu.Lock()
			p.users["alice"].PasswordChangedAt = time.Now().Add(-tc.age)
			p.mu.Unlock()

			u, err := p.Authenticate(ctx, "alice", "Passw0rd!", "123456")
			if tc.wantExpired {
				if err == nil || err.Error() != "auth: password expired" {
					t.Fatalf("Authenticate error = %v, want password expired", err)
				}
				if u != nil {
					t.Errorf("expired authentication returned user: %+v", u)
				}
				if mfa.calls != 0 {
					t.Errorf("MFA calls = %d, want 0", mfa.calls)
				}
				return
			}
			if err != nil {
				t.Fatalf("Authenticate: %v", err)
			}
			if u == nil || u.Username != "alice" {
				t.Errorf("authenticated user = %+v, want alice", u)
			}
			if mfa.calls != 1 {
				t.Errorf("MFA calls = %d, want 1", mfa.calls)
			}
		})
	}
}
