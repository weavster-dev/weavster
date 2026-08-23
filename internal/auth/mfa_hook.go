package auth

import "context"

// ExternalAuthHook is a pluggable hook that can override built-in credential
// validation (spec §2.8.28).
type ExternalAuthHook interface {
	Authenticate(ctx context.Context, username, password string) (bool, error)
}

// MFAHook is a pluggable multi-factor hook invoked after successful primary
// authentication (spec §2.8.28).
type MFAHook interface {
	Verify(ctx context.Context, user *User, code string) error
}
