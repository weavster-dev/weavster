package auth

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestCreateUserInvalidPasswordLeavesStoreUnchanged(t *testing.T) {
	p := NewLocalProvider(Options{Policy: PasswordPolicy{MinLength: 8}})
	ctx := context.Background()

	if err := p.CreateUser(ctx, User{Username: "alice", PasswordHash: "short"}); err == nil {
		t.Fatal("CreateUser() error = nil, want password-policy rejection")
	}
	if _, err := p.GetUser(ctx, "alice"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("GetUser() after rejected create error = %v, want ErrUserNotFound", err)
	}
}

func TestDeleteUserMissing(t *testing.T) {
	p := NewLocalProvider(Options{})

	if err := p.DeleteUser(context.Background(), "missing"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("DeleteUser() error = %v, want ErrUserNotFound", err)
	}
}

func TestChangePasswordRejectionLeavesCredentialsUnchanged(t *testing.T) {
	tests := []struct {
		name        string
		oldPassword string
		newPassword string
		want        error
	}{
		{
			name:        "wrong current password",
			oldPassword: "WrongPass1!",
			newPassword: "NewPassw0rd!",
			want:        ErrPasswordWrong,
		},
		{
			name:        "invalid new password",
			oldPassword: "OldPassw0rd!",
			newPassword: "short",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := NewLocalProvider(Options{Policy: PasswordPolicy{MinLength: 8, ReuseLimit: 3}})
			ctx := context.Background()
			if err := p.CreateUser(ctx, User{Username: "alice", PasswordHash: "OldPassw0rd!"}); err != nil {
				t.Fatalf("CreateUser() error = %v", err)
			}
			before, err := p.GetUser(ctx, "alice")
			if err != nil {
				t.Fatalf("GetUser() before change error = %v", err)
			}
			originalHash := before.PasswordHash
			originalChangedAt := before.PasswordChangedAt
			originalHistory := append([]string(nil), before.PasswordHistory...)

			err = p.ChangePassword(ctx, "alice", tc.oldPassword, tc.newPassword)
			if err == nil {
				t.Fatal("ChangePassword() error = nil, want rejection")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("ChangePassword() error = %v, want %v", err, tc.want)
			}

			after, err := p.GetUser(ctx, "alice")
			if err != nil {
				t.Fatalf("GetUser() after change error = %v", err)
			}
			if after.PasswordHash != originalHash {
				t.Error("rejected password change modified PasswordHash")
			}
			if !after.PasswordChangedAt.Equal(originalChangedAt) {
				t.Error("rejected password change modified PasswordChangedAt")
			}
			if !slices.Equal(after.PasswordHistory, originalHistory) {
				t.Errorf("rejected password change modified PasswordHistory: got %v, want %v", after.PasswordHistory, originalHistory)
			}
		})
	}
}
