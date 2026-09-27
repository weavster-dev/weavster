package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/weavster-dev/weavster/internal/auth"
	"github.com/weavster-dev/weavster/internal/gateway"
)

// validUsername: 1–64 of A-Z a-z 0-9 . _ @ - (no colon: Basic credentials
// split on it).
var validUsername = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,64}$`)

// userAdminAdapter serves user administration from the local provider.
// Changes that could remove the last admin are serialized.
type userAdminAdapter struct {
	p  *auth.LocalProvider
	mu *sync.Mutex
}

func toUserInfo(p *auth.LocalProvider, u auth.User) gateway.UserInfo {
	perms := append([]string{}, u.Permissions...)
	sort.Strings(perms)
	return gateway.UserInfo{Username: u.Username, Email: u.Email, Org: u.Org, Permissions: perms,
		MustChangePassword: u.MustChangePassword, Locked: p.Locked(u)}
}

func (a userAdminAdapter) ListUsers(ctx context.Context) ([]gateway.UserInfo, error) {
	users, err := a.p.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gateway.UserInfo, 0, len(users))
	for _, u := range users {
		out = append(out, toUserInfo(a.p, u))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out, nil
}

func (a userAdminAdapter) GetUser(ctx context.Context, username string) (gateway.UserInfo, error) {
	u, err := a.p.GetUser(ctx, username)
	if err != nil {
		return gateway.UserInfo{}, userErr(err)
	}
	return toUserInfo(a.p, *u), nil
}

// checkPermissions rejects permissions outside the documented set.
func checkPermissions(perms []string) error {
	known := auth.KnownPermissions()
	for _, p := range perms {
		if !slices.Contains(known, p) {
			return fmt.Errorf("%w: unknown permission %q (known: %v)", gateway.ErrInvalidUser, p, known)
		}
	}
	return nil
}

func (a userAdminAdapter) CreateUser(ctx context.Context, nu gateway.NewUser) (gateway.UserInfo, error) {
	if !validUsername.MatchString(nu.Username) || strings.Trim(nu.Username, ".") == "" {
		return gateway.UserInfo{}, fmt.Errorf("%w: username must be 1-64 characters from A-Z a-z 0-9 . _ @ -", gateway.ErrInvalidUser)
	}
	if err := checkPermissions(nu.Permissions); err != nil {
		return gateway.UserInfo{}, err
	}
	must := nu.MustChangePassword == nil || *nu.MustChangePassword
	err := a.p.CreateUser(ctx, auth.User{Username: nu.Username, PasswordHash: nu.Password, Email: nu.Email, Org: nu.Org,
		Permissions: nu.Permissions, MustChangePassword: must})
	if err != nil {
		return gateway.UserInfo{}, userErr(err)
	}
	return a.GetUser(ctx, nu.Username)
}

func (a userAdminAdapter) UpdateUser(ctx context.Context, username string, uu gateway.UserUpdate) (gateway.UserInfo, error) {
	perms := []string{}
	if uu.Permissions != nil {
		perms = *uu.Permissions
	}
	if err := checkPermissions(perms); err != nil {
		return gateway.UserInfo{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	current, err := a.p.GetUser(ctx, username)
	if err != nil {
		return gateway.UserInfo{}, userErr(err)
	}
	if !slices.Contains(perms, auth.PermAdmin) {
		if err := a.keepAnAdmin(ctx, username); err != nil {
			return gateway.UserInfo{}, err
		}
	}
	next := auth.User{Email: current.Email, Org: current.Org, Permissions: perms}
	if uu.Email != nil {
		next.Email = *uu.Email
	}
	if uu.Org != nil {
		next.Org = *uu.Org
	}
	if err := a.p.UpdateUser(ctx, username, next); err != nil {
		return gateway.UserInfo{}, userErr(err)
	}
	return a.GetUser(ctx, username)
}

func (a userAdminAdapter) DeleteUser(ctx context.Context, username string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.keepAnAdmin(ctx, username); err != nil {
		return err
	}
	return userErr(a.p.DeleteUser(ctx, username))
}

// keepAnAdmin refuses when username is the only account with admin.
func (a userAdminAdapter) keepAnAdmin(ctx context.Context, username string) error {
	users, err := a.p.ListUsers(ctx)
	if err != nil {
		return err
	}
	others := 0
	isAdmin := false
	for _, u := range users {
		if slices.Contains(u.Permissions, auth.PermAdmin) {
			if u.Username == username {
				isAdmin = true
			} else {
				others++
			}
		}
	}
	if isAdmin && others == 0 {
		return gateway.ErrLastAdmin
	}
	return nil
}

func (a userAdminAdapter) SetPassword(ctx context.Context, username, password string) error {
	return userErr(a.p.SetPassword(ctx, username, password))
}

// userErr translates the provider's errors: a password the policy rejects
// is invalid input with its reason; anything else unexpected stays an
// internal error (500, no detail shown).
func userErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, auth.ErrUserNotFound):
		return gateway.ErrUserNotFound
	case errors.Is(err, auth.ErrUserExists):
		return gateway.ErrUserExists
	case errors.Is(err, auth.ErrPasswordPolicy):
		return fmt.Errorf("%w: %w", gateway.ErrInvalidUser, err)
	}
	return err
}
