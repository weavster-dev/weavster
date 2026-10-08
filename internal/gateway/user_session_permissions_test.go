package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

type updatedPermissionsUsers struct{ fakeUsers }

func (updatedPermissionsUsers) UpdateUser(_ context.Context, name string, u UserUpdate, _ bool) (UserInfo, error) {
	return UserInfo{Username: name, Permissions: *u.Permissions}, nil
}

func TestSelfUpdateSessionPermissions(t *testing.T) {
	for _, tc := range []struct {
		name        string
		permissions []string
		want        int
	}{
		{"unchanged", []string{"users:admin", "flows:view"}, http.StatusOK},
		{"reordered", []string{"flows:view", "users:admin"}, http.StatusOK},
		{"duplicate", []string{"users:admin", "flows:view", "flows:view"}, http.StatusOK},
		{"removed view", []string{"users:admin"}, http.StatusUnauthorized},
		{"removed user administration", []string{"flows:view"}, http.StatusUnauthorized},
		{"removed all", []string{}, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New(Config{Auth: fakeAuth{}, Authorizer: fakeAuthz{}, Users: updatedPermissionsUsers{}})
			id := Identity{Username: "u", Permissions: []string{"users:admin", "flows:view"}}
			create := func() string {
				t.Helper()
				token, err := s.sessions.create(id, time.Now())
				if err != nil {
					t.Fatal(err)
				}
				return token
			}
			own, other := create(), create()
			setToken := func(token string) func(*http.Request) {
				return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
			}
			body, err := json.Marshal(UserUpdate{Permissions: &tc.permissions})
			if err != nil {
				t.Fatal(err)
			}
			if rec := serve(s, http.MethodPut, "/api/v1/users/u", string(body), setToken(own)); rec.Code != http.StatusOK {
				t.Fatalf("self update: status %d", rec.Code)
			}
			if rec := serve(s, http.MethodGet, "/api/v1/auth/me", "", setToken(own)); rec.Code != tc.want {
				t.Errorf("own session: status %d, want %d", rec.Code, tc.want)
			}
			if rec := serve(s, http.MethodGet, "/api/v1/auth/me", "", setToken(other)); rec.Code != http.StatusUnauthorized {
				t.Errorf("other session: status %d, want 401", rec.Code)
			}
		})
	}
}
