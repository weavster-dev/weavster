// Package auth implements the AuthProvider and Authorizer ports (local user
// store and permission set) plus password policy, lockout, anti-enumeration,
// and the MFA hook.
package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Common errors.
var (
	ErrUserNotFound  = errors.New("auth: user not found")
	ErrUserExists    = errors.New("auth: user already exists")
	ErrPasswordWrong = errors.New("auth: invalid credentials")
	// ErrPasswordConflict means the password changed while a change was in flight.
	ErrPasswordConflict = errors.New("auth: password was changed concurrently; retry")
)

// User is a local account.
type User struct {
	ID                string
	Username          string
	Org               string
	Email             string
	PasswordHash      string
	PasswordChangedAt time.Time
	PasswordHistory   []string
	FailedAttempts    int
	LockedUntil       time.Time
	Permissions       []string
	// MustChangePassword blocks API use until the user changes their
	// password (first-run bootstrap, D-22). ChangePassword clears it.
	MustChangePassword bool
}

// AuthProvider is the port for identity/authentication (arch §3.1).
type AuthProvider interface {
	Authenticate(ctx context.Context, username, password, mfaCode string) (*User, error)
	CreateUser(ctx context.Context, u User) error
	UpdateUser(ctx context.Context, username string, u User) error
	DeleteUser(ctx context.Context, username string) error
	GetUser(ctx context.Context, username string) (*User, error)
	ListUsers(ctx context.Context) ([]User, error)
	ChangePassword(ctx context.Context, username, oldPassword, newPassword string) error
}

// Options configures the local auth provider.
type Options struct {
	Policy          PasswordPolicy
	Lockout         LockoutPolicy
	AntiEnumeration bool
	External        ExternalAuthHook
	MFA             MFAHook
}

// LocalProvider is the MVP AuthProvider adapter (local user store).
type LocalProvider struct {
	mu    sync.Mutex
	users map[string]*User
	opts  Options
}

// NewLocalProvider returns an empty local user store with the given options.
func NewLocalProvider(opts Options) *LocalProvider {
	return &LocalProvider{users: make(map[string]*User), opts: opts}
}

func (p *LocalProvider) Authenticate(ctx context.Context, username, password, mfaCode string) (*User, error) {
	p.mu.Lock()
	u, ok := p.users[username]
	if !ok {
		p.mu.Unlock()
		return nil, p.genericOr(ErrUserNotFound)
	}

	if p.isLocked(u) {
		p.mu.Unlock()
		return nil, p.genericOr(ErrPasswordWrong)
	}

	// External auth hook overrides built-in credential validation.
	if p.opts.External != nil {
		snapshot := u.clone()
		p.mu.Unlock()
		ok, err := p.opts.External.Authenticate(ctx, username, password)
		if err != nil || !ok {
			return nil, p.genericOr(ErrPasswordWrong)
		}
		return p.finishAuth(ctx, &snapshot, mfaCode)
	}

	// Verify outside the lock: the Argon2id hash is deliberately expensive
	// and must not serialize every authentication.
	hash := u.PasswordHash
	p.mu.Unlock()
	valid := VerifyPassword(hash, password)
	p.mu.Lock()
	// Revalidate: the account may have been deleted, re-passworded, or
	// locked while the hash ran.
	if current, ok := p.users[username]; !ok || current != u || u.PasswordHash != hash || p.isLocked(u) {
		p.mu.Unlock()
		return nil, p.genericOr(ErrPasswordWrong)
	}
	if !valid {
		p.recordFailure(u)
		p.mu.Unlock()
		return nil, p.genericOr(ErrPasswordWrong)
	}

	p.recordSuccess(u)

	// Password expiration/grace enforcement.
	if err := p.opts.Policy.CheckExpired(u.PasswordChangedAt); err != nil {
		p.mu.Unlock()
		return nil, err
	}
	// Return a copy so callers never read the shared record without the lock.
	snapshot := u.clone()
	p.mu.Unlock()

	return p.finishAuth(ctx, &snapshot, mfaCode)
}

// clone returns a deep copy of u, so callers can never share its slices.
func (u *User) clone() User {
	c := *u
	c.Permissions = append([]string(nil), u.Permissions...)
	c.PasswordHistory = append([]string(nil), u.PasswordHistory...)
	return c
}

// finishAuth runs the MFA hook after successful primary authentication.
func (p *LocalProvider) finishAuth(ctx context.Context, u *User, mfaCode string) (*User, error) {
	if p.opts.MFA != nil {
		if err := p.opts.MFA.Verify(ctx, u, mfaCode); err != nil {
			return nil, fmt.Errorf("auth: mfa: %w", err)
		}
	}
	return u, nil
}

func (p *LocalProvider) genericOr(err error) error {
	if p.opts.AntiEnumeration {
		return ErrGenericFailure // generic message, no account-specific detail
	}
	return err
}

func (p *LocalProvider) recordFailure(u *User) {
	now := time.Now()
	if p.opts.Lockout.expired(u.LockedUntil, now) {
		u.FailedAttempts = 0 // strike decay
	}
	u.FailedAttempts++
	if p.opts.Lockout.RetryLimit > 0 && u.FailedAttempts >= p.opts.Lockout.RetryLimit {
		u.LockedUntil = now.Add(time.Duration(p.opts.Lockout.LockoutPeriod) * time.Second)
		u.FailedAttempts = 0
	}
}

func (p *LocalProvider) recordSuccess(u *User) {
	u.FailedAttempts = 0
	u.LockedUntil = time.Time{}
}

func (p *LocalProvider) isLocked(u *User) bool {
	return p.opts.Lockout.isLocked(u.LockedUntil)
}

func (p *LocalProvider) CreateUser(ctx context.Context, u User) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.users[u.Username]; ok {
		return ErrUserExists
	}
	// u.PasswordHash carries the plaintext password on creation.
	if err := p.opts.Policy.Validate(u.PasswordHash); err != nil {
		return err
	}
	hash, err := HashPassword(u.PasswordHash)
	if err != nil {
		return err
	}
	u.PasswordHash = hash
	u.PasswordChangedAt = time.Now()
	u.PasswordHistory = []string{hash}
	p.users[u.Username] = &u
	return nil
}

func (p *LocalProvider) UpdateUser(ctx context.Context, username string, u User) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	existing, ok := p.users[username]
	if !ok {
		return ErrUserNotFound
	}
	u.PasswordHash = existing.PasswordHash
	u.PasswordChangedAt = existing.PasswordChangedAt
	u.PasswordHistory = existing.PasswordHistory
	p.users[username] = &u
	return nil
}

func (p *LocalProvider) DeleteUser(ctx context.Context, username string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.users[username]; !ok {
		return ErrUserNotFound
	}
	delete(p.users, username)
	return nil
}

func (p *LocalProvider) GetUser(ctx context.Context, username string) (*User, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	u, ok := p.users[username]
	if !ok {
		return nil, ErrUserNotFound
	}
	return u, nil
}

func (p *LocalProvider) ListUsers(ctx context.Context) ([]User, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]User, 0, len(p.users))
	for _, u := range p.users {
		out = append(out, *u)
	}
	return out, nil
}

// ChangePassword verifies oldPassword and sets newPassword. The Argon2id work
// runs outside the provider lock; the record is revalidated before the
// change is applied.
func (p *LocalProvider) ChangePassword(ctx context.Context, username, oldPassword, newPassword string) error {
	p.mu.Lock()
	u, ok := p.users[username]
	if !ok {
		p.mu.Unlock()
		return ErrUserNotFound
	}
	if p.isLocked(u) {
		p.mu.Unlock()
		return ErrPasswordWrong
	}
	oldHash, history := u.PasswordHash, append([]string(nil), u.PasswordHistory...)
	p.mu.Unlock()

	if !VerifyPassword(oldHash, oldPassword) {
		p.mu.Lock()
		p.recordFailure(u)
		p.mu.Unlock()
		return ErrPasswordWrong
	}
	if newPassword == oldPassword {
		return ErrPasswordReused
	}
	if err := p.opts.Policy.Validate(newPassword); err != nil {
		return err
	}
	if p.opts.Policy.reused(newPassword, history) {
		return ErrPasswordReused
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if current, ok := p.users[username]; !ok || current != u || u.PasswordHash != oldHash {
		return ErrPasswordConflict
	}
	u.PasswordHash = hash
	u.PasswordChangedAt = time.Now()
	u.MustChangePassword = false
	u.PasswordHistory = append([]string{hash}, u.PasswordHistory...)
	if len(u.PasswordHistory) > p.opts.Policy.ReuseLimit {
		u.PasswordHistory = u.PasswordHistory[:p.opts.Policy.ReuseLimit]
	}
	return nil
}

var _ AuthProvider = (*LocalProvider)(nil)
