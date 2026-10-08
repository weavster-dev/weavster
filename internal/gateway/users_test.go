package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeUsers knows the user "u"; "busy" fails with a store error, "last"
// is the last admin, and "bad" breaks the password policy.
type fakeUsers struct{}

func (fakeUsers) ListUsers(context.Context) ([]UserInfo, error) {
	return []UserInfo{{Username: "u", Permissions: []string{}}}, nil
}

func (fakeUsers) GetUser(_ context.Context, name string) (UserInfo, error) {
	switch name {
	case "u":
		return UserInfo{Username: "u"}, nil
	case "busy":
		return UserInfo{}, errors.New("disk")
	}
	return UserInfo{}, ErrUserNotFound
}

func (fakeUsers) CreateUser(_ context.Context, nu NewUser) (UserInfo, error) {
	switch nu.Username {
	case "u":
		return UserInfo{}, ErrUserExists
	case "bad":
		return UserInfo{}, errors.Join(ErrInvalidUser, errors.New("too short"))
	}
	return UserInfo{Username: nu.Username}, nil
}

func (fakeUsers) UpdateUser(_ context.Context, name string, _ UserUpdate, _ bool) (UserInfo, error) {
	if name == "last" {
		return UserInfo{}, ErrLastAdmin
	}
	return UserInfo{Username: name}, nil
}

func (fakeUsers) DeleteUser(_ context.Context, name string, _ bool) error {
	if name == "nobody" {
		return ErrUserNotFound
	}
	return nil
}

func (fakeUsers) SetPassword(_ context.Context, name, _ string, _ bool) error {
	if name == "nobody" {
		return ErrUserNotFound
	}
	return nil
}

func TestUserHandlers(t *testing.T) {
	cfg := Config{Users: fakeUsers{}}
	tests := []struct {
		name, method, path, body string
		status                   int
		want                     string
	}{
		{"list", http.MethodGet, "/api/v1/users", ``, http.StatusOK, `"username":"u"`},
		{"get", http.MethodGet, "/api/v1/users/u", ``, http.StatusOK, `"username":"u"`},
		{"get unknown", http.MethodGet, "/api/v1/users/x", ``, http.StatusNotFound, "user not found"},
		{"get store error", http.MethodGet, "/api/v1/users/busy", ``, http.StatusInternalServerError, "internal error"},
		{"create", http.MethodPost, "/api/v1/users", `{"username":"n","password":"p","permissions":[]}`, http.StatusCreated, `"username":"n"`},
		{"create exists", http.MethodPost, "/api/v1/users", `{"username":"u"}`, http.StatusConflict, "user already exists"},
		{"create invalid", http.MethodPost, "/api/v1/users", `{"username":"bad"}`, http.StatusBadRequest, "too short"},
		{"create bad json", http.MethodPost, "/api/v1/users", `{`, http.StatusBadRequest, "invalid JSON body"},
		{"update", http.MethodPut, "/api/v1/users/u", `{"permissions":["flows:view"]}`, http.StatusOK, `"username":"u"`},
		{"update last admin", http.MethodPut, "/api/v1/users/last", `{"permissions":[]}`, http.StatusConflict, "last account"},
		{"update bad json", http.MethodPut, "/api/v1/users/u", `[`, http.StatusBadRequest, "invalid JSON body"},
		{"update without permissions", http.MethodPut, "/api/v1/users/u", `{"email":"x@y"}`, http.StatusBadRequest, "permissions is required"},
		{"delete", http.MethodDelete, "/api/v1/users/u", ``, http.StatusNoContent, ""},
		{"delete unknown", http.MethodDelete, "/api/v1/users/nobody", ``, http.StatusNotFound, "user not found"},
		{"set password", http.MethodPost, "/api/v1/users/u/password", `{"password":"x"}`, http.StatusNoContent, ""},
		{"set password unknown", http.MethodPost, "/api/v1/users/nobody/password", `{"password":"x"}`, http.StatusNotFound, "user not found"},
		{"set password bad json", http.MethodPost, "/api/v1/users/u/password", `{"pass":1}`, http.StatusBadRequest, "invalid JSON body"},
		{"trailing data", http.MethodPost, "/api/v1/users", `{"username":"n"} {"x":1}`, http.StatusBadRequest, "trailing data"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(cfg).Router().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("got %d %q; want %d containing %q", rec.Code, rec.Body.String(), tt.status, tt.want)
			}
		})
	}
}

// TestSessionsRevokedDuringLogin: a session whose credentials were checked
// before a revocation of the user is not valid, even when created after it;
// the kept token of the revoking user stays valid.
func TestSessionsRevokedDuringLogin(t *testing.T) {
	s := newSessions()
	checked := time.Now()
	keep, _ := s.create(Identity{Username: "u"}, checked)
	s.revokeUser("u", keep)
	late, _ := s.create(Identity{Username: "u"}, checked) // login in flight during the revoke
	if _, ok := s.lookup(late); ok {
		t.Error("a session authenticated before the revocation is valid")
	}
	if _, ok := s.lookup(keep); !ok {
		t.Error("the kept session was revoked")
	}
	fresh, _ := s.create(Identity{Username: "u"}, time.Now().Add(time.Millisecond))
	if _, ok := s.lookup(fresh); !ok {
		t.Error("a session authenticated after the revocation is not valid")
	}
}
