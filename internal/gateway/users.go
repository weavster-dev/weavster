package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
)

// UserInfo is a user account as the API shows it (never password data).
type UserInfo struct {
	Username           string   `json:"username"`
	Email              string   `json:"email,omitempty"`
	Org                string   `json:"org,omitempty"`
	Permissions        []string `json:"permissions"`
	MustChangePassword bool     `json:"mustChangePassword"`
	Locked             bool     `json:"locked"`
}

// NewUser is the body of POST /api/v1/users.
type NewUser struct {
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	Email       string   `json:"email,omitempty"`
	Org         string   `json:"org,omitempty"`
	Permissions []string `json:"permissions"`
	// MustChangePassword defaults to true: the user picks their own
	// password at the first login.
	MustChangePassword *bool `json:"mustChangePassword,omitempty"`
}

// UserUpdate is the body of PUT /api/v1/users/{name}: Permissions is
// required; Email and Org are kept when absent.
type UserUpdate struct {
	Email       *string   `json:"email,omitempty"`
	Org         *string   `json:"org,omitempty"`
	Permissions *[]string `json:"permissions"`
}

// UserAdmin manages user accounts.
type UserAdmin interface {
	ListUsers(ctx context.Context) ([]UserInfo, error)
	GetUser(ctx context.Context, username string) (UserInfo, error)
	CreateUser(ctx context.Context, u NewUser) (UserInfo, error)
	// UpdateUser, DeleteUser, and SetPassword refuse an account that has
	// admin unless asAdmin (ErrAdminTarget), checked atomically with the
	// change.
	UpdateUser(ctx context.Context, username string, u UserUpdate, asAdmin bool) (UserInfo, error)
	DeleteUser(ctx context.Context, username string, asAdmin bool) error
	// SetPassword sets a new password the user must change at next login.
	SetPassword(ctx context.Context, username, password string, asAdmin bool) error
}

// User administration errors.
var (
	ErrUserNotFound = errors.New("user not found")
	ErrUserExists   = errors.New("user already exists")
	// ErrInvalidUser: a bad username, permission, or password (policy);
	// wrapped with a message that is safe to show.
	ErrInvalidUser = errors.New("invalid user")
	// ErrAdminTarget: only an account with admin may change an account
	// that has admin.
	ErrAdminTarget = errors.New("only an account with admin can change an account that has admin")
	// ErrLastAdmin: the change would leave no account with admin.
	ErrLastAdmin = errors.New("the last account with the admin permission cannot be removed or lose it")
)

func writeUserError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUserNotFound):
		writeStatusError(w, http.StatusNotFound, "user not found")
	case errors.Is(err, ErrUserExists):
		writeStatusError(w, http.StatusConflict, "user already exists")
	case errors.Is(err, ErrAdminTarget):
		writeStatusError(w, http.StatusForbidden, ErrAdminTarget.Error())
	case errors.Is(err, ErrLastAdmin):
		writeStatusError(w, http.StatusConflict, ErrLastAdmin.Error())
	case errors.Is(err, ErrInvalidUser):
		writeStatusError(w, http.StatusBadRequest, err.Error())
	default:
		writeBackendError(w, err)
	}
}

// decodeStrict decodes a JSON body into v, rejecting unknown fields.
func decodeStrict(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := decodeJSON(w, r, v); err != nil {
		writeStatusError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

// decodeJSON decodes one JSON document (at most 1 MiB, no unknown fields)
// into v; an empty body is io.EOF.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing data after the JSON document")
	}
	return nil
}

// permAdmin is the permission that allows everything.
const permAdmin = "admin"

// mayGrant checks that the caller may give these permissions: a caller
// without admin can only grant permissions it holds itself, never admin.
// Without authentication everything is allowed.
func mayGrant(r *http.Request, perms []string) error {
	id, ok := IdentityFrom(r.Context())
	if !ok || slices.Contains(id.Permissions, permAdmin) {
		return nil
	}
	for _, p := range perms {
		if p == permAdmin || !slices.Contains(id.Permissions, p) {
			return fmt.Errorf("you cannot grant %s: only an account with admin can grant permissions it does not hold, and only admin can grant admin", p)
		}
	}
	return nil
}

// asAdmin reports whether the caller has admin (or authentication is off).
func asAdmin(r *http.Request) bool {
	id, ok := IdentityFrom(r.Context())
	return !ok || slices.Contains(id.Permissions, permAdmin)
}

// guard checks the permissions the caller would grant; on refusal it
// answers and returns false. (Whether the caller may change the target
// account is checked by the UserAdmin, atomically with the change.)
func (s *Server) guard(w http.ResponseWriter, r *http.Request, perms []string) bool {
	if err := mayGrant(r, perms); err != nil {
		writeStatusError(w, http.StatusForbidden, err.Error())
		return false
	}
	return true
}

// ownToken preserves the caller's session only when its permissions are
// unchanged. A permission change must invalidate the cached identity.
func ownToken(r *http.Request, name string, permissions []string) string {
	if id, ok := IdentityFrom(r.Context()); ok && id.Username == name {
		before, after := slices.Clone(id.Permissions), slices.Clone(permissions)
		slices.Sort(before)
		slices.Sort(after)
		if slices.Equal(slices.Compact(before), slices.Compact(after)) {
			return bearerToken(r)
		}
	}
	return ""
}

func (s *Server) usersAvailable(w http.ResponseWriter) bool {
	if s.cfg.Users == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "user administration unavailable")
		return false
	}
	return true
}

func (s *Server) handleUsersList(w http.ResponseWriter, r *http.Request) {
	if !s.usersAvailable(w) {
		return
	}
	users, err := s.cfg.Users.ListUsers(r.Context())
	if err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) handleUserGet(w http.ResponseWriter, r *http.Request) {
	if !s.usersAvailable(w) {
		return
	}
	u, err := s.cfg.Users.GetUser(r.Context(), r.PathValue("name"))
	if err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	if !s.usersAvailable(w) {
		return
	}
	var nu NewUser
	if !decodeStrict(w, r, &nu) || !s.guard(w, r, nu.Permissions) {
		return
	}
	u, err := s.cfg.Users.CreateUser(r.Context(), nu)
	if err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) handleUserUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.usersAvailable(w) {
		return
	}
	var uu UserUpdate
	if !decodeStrict(w, r, &uu) {
		return
	}
	if uu.Permissions == nil {
		writeStatusError(w, http.StatusBadRequest, "permissions is required (send [] for none)")
		return
	}
	name := r.PathValue("name")
	if !s.guard(w, r, *uu.Permissions) {
		return
	}
	u, err := s.cfg.Users.UpdateUser(r.Context(), name, uu, asAdmin(r))
	if err != nil {
		writeUserError(w, err)
		return
	}
	s.sessions.revokeUser(name, ownToken(r, name, u.Permissions)) // new permissions apply from the next login
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	if !s.usersAvailable(w) {
		return
	}
	name := r.PathValue("name")
	if id, ok := IdentityFrom(r.Context()); ok && id.Username == name {
		writeStatusError(w, http.StatusConflict, "you cannot delete your own account")
		return
	}
	if err := s.cfg.Users.DeleteUser(r.Context(), name, asAdmin(r)); err != nil {
		writeUserError(w, err)
		return
	}
	s.sessions.revokeUser(name, "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUserSetPassword(w http.ResponseWriter, r *http.Request) {
	if !s.usersAvailable(w) {
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if !decodeStrict(w, r, &body) {
		return
	}
	name := r.PathValue("name")
	if err := s.cfg.Users.SetPassword(r.Context(), name, body.Password, asAdmin(r)); err != nil {
		writeUserError(w, err)
		return
	}
	s.sessions.revokeUser(name, "") // every session, the caller's own too: the old password is gone
	w.WriteHeader(http.StatusNoContent)
}
