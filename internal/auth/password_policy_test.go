package auth

import (
	"testing"
	"time"
)

func TestPasswordPolicyCheckExpired(t *testing.T) {
	p := PasswordPolicy{Expiration: 30}

	// Password changed 31 days ago — should be expired.
	if err := p.CheckExpired(time.Now().Add(-31 * 24 * time.Hour)); err == nil {
		t.Error("expected expired error, got nil")
	}

	// Password changed today — should be valid.
	if err := p.CheckExpired(time.Now()); err != nil {
		t.Errorf("unexpected error for fresh password: %v", err)
	}

	// No expiration configured (0) — never expires.
	noExp := PasswordPolicy{Expiration: 0}
	if err := noExp.CheckExpired(time.Now().Add(-365 * 24 * time.Hour)); err != nil {
		t.Errorf("no-expiration policy should not expire: %v", err)
	}

	// Within grace period (expired but within grace days) — still expired (no grace in CheckExpired).
	gracePol := PasswordPolicy{Expiration: 10}
	if err := gracePol.CheckExpired(time.Now().Add(-11 * 24 * time.Hour)); err == nil {
		t.Error("expected expired error past expiration threshold")
	}
}

func TestPasswordPolicyReuse(t *testing.T) {
	p := PasswordPolicy{ReuseLimit: 3}

	hash1, err := HashPassword("OldPass1!")
	if err != nil {
		t.Fatal(err)
	}
	hash2, err := HashPassword("OldPass2!")
	if err != nil {
		t.Fatal(err)
	}

	history := []string{hash1, hash2}

	// Reusing an old password must be detected.
	if !p.reused("OldPass1!", history) {
		t.Error("expected reuse detection for OldPass1!")
	}
	if !p.reused("OldPass2!", history) {
		t.Error("expected reuse detection for OldPass2!")
	}

	// A new password must not be flagged as reused.
	if p.reused("BrandNew3#", history) {
		t.Error("brand-new password should not be flagged as reused")
	}

	// ReuseLimit of 0 disables reuse checking entirely.
	noLimit := PasswordPolicy{ReuseLimit: 0}
	if noLimit.reused("OldPass1!", history) {
		t.Error("ReuseLimit=0 should not check history")
	}
}

func TestPasswordPolicyValidateEdgeCases(t *testing.T) {
	cases := []struct {
		name   string
		policy PasswordPolicy
		pass   string
		wantOK bool
	}{
		{"forbid uppercase", PasswordPolicy{MinLength: 4, MinUpper: -1}, "lowpass1", true},
		{"forbid uppercase violated", PasswordPolicy{MinLength: 4, MinUpper: -1}, "Lowpass1", false},
		{"forbid lowercase", PasswordPolicy{MinLength: 4, MinLower: -1}, "UPPASS1", true},
		{"forbid lowercase violated", PasswordPolicy{MinLength: 4, MinLower: -1}, "UPPASSa1", false},
		{"forbid digits", PasswordPolicy{MinLength: 4, MinNumeric: -1}, "abcd!", true},
		{"forbid digits violated", PasswordPolicy{MinLength: 4, MinNumeric: -1}, "abcd1", false},
		{"forbid specials", PasswordPolicy{MinLength: 4, MinSpecial: -1}, "Abc1", true},
		{"forbid specials violated", PasswordPolicy{MinLength: 4, MinSpecial: -1}, "Abc1!", false},
		{"too short", PasswordPolicy{MinLength: 10}, "short", false},
		{"char class req unmet", PasswordPolicy{MinLength: 4, MinUpper: 2, MinLower: 2, MinNumeric: 1, MinSpecial: 1}, "Aa1!", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.policy.Validate(tc.pass)
			if tc.wantOK && err != nil {
				t.Errorf("expected valid, got: %v", err)
			}
			if !tc.wantOK && err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}
