package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	UpdateUser(ctx context.Context, username string, u UserUpdate) (UserInfo, error)
	DeleteUser(ctx context.Context, username string) error
	// SetPassword sets a new password the user must change at next login.
	SetPassword(ctx context.Context, username, password string) error
}

// User administration errors.
var (
	ErrUserNotFound = errors.New("user not found")
	ErrUserExists   = errors.New("user already exists")
	// ErrInvalidUser: a bad username, permission, or password (policy);
	// wrapped with a message that is safe to show.
	ErrInvalidUser = errors.New("invalid user")
	// ErrLastAdmin: the change would leave no account with admin.
	ErrLastAdmin = errors.New("the last account with the admin permission cannot be removed or lose it")
)

func writeUserError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUserNotFound):
		writeStatusError(w, http.StatusNotFound, "user not found")
	case errors.Is(err, ErrUserExists):
		writeStatusError(w, http.StatusConflict, "user already exists")
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
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeStatusError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
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

// mayManage checks that the caller may change the account name: only an
// account with admin may change an account that has admin.
func (s *Server) mayManage(r *http.Request, name string) (int, error) {
	id, ok := IdentityFrom(r.Context())
	if !ok || slices.Contains(id.Permissions, permAdmin) {
		return 0, nil
	}
	target, err := s.cfg.Users.GetUser(r.Context(), name)
	if err != nil {
		return 0, err
	}
	if slices.Contains(target.Permissions, permAdmin) {
		return http.StatusForbidden, errors.New("only an account with admin can change an account that has admin")
	}
	return 0, nil
}

// guard runs the escalation checks; on refusal it answers and returns false.
func (s *Server) guard(w http.ResponseWriter, r *http.Request, name string, perms []string) bool {
	if name != "" {
		if status, err := s.mayManage(r, name); err != nil {
			if status != 0 {
				writeStatusError(w, status, err.Error())
			} else {
				writeUserError(w, err)
			}
			return false
		}
	}
	if err := mayGrant(r, perms); err != nil {
		writeStatusError(w, http.StatusForbidden, err.Error())
		return false
	}
	return true
}

// ownToken is the caller's bearer token when it manages its own account,
// so a change to it does not end the caller's own session.
func ownToken(r *http.Request, name string) string {
	if id, ok := IdentityFrom(r.Context()); ok && id.Username == name {
		return bearerToken(r)
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
	if !decodeStrict(w, r, &nu) || !s.guard(w, r, "", nu.Permissions) {
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
	if !s.guard(w, r, name, *uu.Permissions) {
		return
	}
	u, err := s.cfg.Users.UpdateUser(r.Context(), name, uu)
	if err != nil {
		writeUserError(w, err)
		return
	}
	s.sessions.revokeUser(name, ownToken(r, name)) // new permissions apply from the next login
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
	if !s.guard(w, r, name, nil) {
		return
	}
	if err := s.cfg.Users.DeleteUser(r.Context(), name); err != nil {
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
	if !s.guard(w, r, name, nil) {
		return
	}
	if err := s.cfg.Users.SetPassword(r.Context(), name, body.Password); err != nil {
		writeUserError(w, err)
		return
	}
	s.sessions.revokeUser(name, ownToken(r, name))
	w.WriteHeader(http.StatusNoContent)
}
