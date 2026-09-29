package gateway

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// fakePrefs keeps preferences in memory; "ghost" is unknown.
type fakePrefs struct{ m map[string]map[string]string }

func (f fakePrefs) Preferences(_ context.Context, u string) (map[string]string, error) {
	if u == "ghost" {
		return nil, ErrUserNotFound
	}
	return f.m[u], nil
}

func (f fakePrefs) SetPreferences(_ context.Context, u string, p map[string]string, asAdmin bool) error {
	switch {
	case u == "ghost":
		return ErrUserNotFound
	case u == "boss" && !asAdmin:
		return ErrAdminTarget
	}
	f.m[u] = p
	return nil
}

// fakeChecker accepts passwords of at least 8 characters.
type fakeChecker struct{}

func (fakeChecker) CheckPassword(pw string) error {
	if len(pw) < 8 {
		return errors.New("auth: password shorter than 8 characters")
	}
	return nil
}

func TestUserExtras(t *testing.T) {
	prefs := fakePrefs{m: map[string]map[string]string{"alice": {"theme": "dark"}}}
	s := New(Config{
		Auth: fakeAuth{users: map[string]Identity{
			"alice": {Username: "alice", Permissions: []string{"flows:view"}},
			"bob":   {Username: "bob"},
			"root":  {Username: "root", Permissions: []string{"users:admin"}},
			"boss":  {Username: "boss", Permissions: []string{"admin"}},
			"fresh": {Username: "fresh", MustChangePassword: true},
		}},
		Authorizer:    fakeAuthz{},
		Preferences:   prefs,
		PasswordCheck: fakeChecker{},
	})
	as := func(u string) func(*http.Request) { return func(r *http.Request) { r.SetBasicAuth(u, "pw") } }
	big := `{"k":"` + strings.Repeat("x", maxPreferenceValue+1) + `"}`
	many := "{"
	for i := range maxPreferences + 1 {
		if i > 0 {
			many += ","
		}
		many += `"k` + strings.Repeat("x", i%3) + string(rune('a'+i%26)) + string(rune('a'+i/26)) + `":"v"`
	}
	many += "}"
	for _, tt := range []struct {
		name, method, path, body, user string
		status                         int
		want                           string
	}{
		{"own preferences", http.MethodGet, "/api/v1/users/alice/preferences", "", "alice", http.StatusOK, `"theme":"dark"`},
		{"set own", http.MethodPut, "/api/v1/users/alice/preferences", `{"theme":"light","lang":"en"}`, "alice", http.StatusOK, `"lang":"en"`},
		{"another's", http.MethodGet, "/api/v1/users/alice/preferences", "", "bob", http.StatusForbidden, "users:admin"},
		{"another's as admin", http.MethodGet, "/api/v1/users/alice/preferences", "", "root", http.StatusOK, `"theme":"light"`},
		{"unknown user", http.MethodGet, "/api/v1/users/ghost/preferences", "", "root", http.StatusNotFound, "user not found"},
		{"set unknown", http.MethodPut, "/api/v1/users/ghost/preferences", `{}`, "root", http.StatusNotFound, "user not found"},
		{"not an object", http.MethodPut, "/api/v1/users/alice/preferences", `["x"]`, "alice", http.StatusBadRequest, "invalid JSON body"},
		{"a number", http.MethodPut, "/api/v1/users/alice/preferences", `{"n":1}`, "alice", http.StatusBadRequest, "invalid JSON body"},
		{"too long a value", http.MethodPut, "/api/v1/users/alice/preferences", big, "alice", http.StatusBadRequest, "at most 4096 bytes"},
		{"an empty name", http.MethodPut, "/api/v1/users/alice/preferences", `{"":"v"}`, "alice", http.StatusBadRequest, "1-100 characters"},
		{"trailing data", http.MethodPut, "/api/v1/users/alice/preferences", `{"a":"b"}{"c":"d"}`, "alice", http.StatusBadRequest, "trailing data"},
		{"null", http.MethodPut, "/api/v1/users/alice/preferences", `null`, "alice", http.StatusBadRequest, "JSON object"},
		{"an admin's as users:admin", http.MethodPut, "/api/v1/users/boss/preferences", `{}`, "root", http.StatusForbidden, "account that has admin"},
		{"your own as an admin", http.MethodPut, "/api/v1/users/boss/preferences", `{"a":"b"}`, "boss", http.StatusOK, `"a":"b"`},
		{"too many", http.MethodPut, "/api/v1/users/alice/preferences", many, "alice", http.StatusBadRequest, "at most 100 preferences"},
		{"logged in", http.MethodGet, "/api/v1/users/alice/loggedin", "", "alice", http.StatusOK, `"loggedIn":false`},
		{"another's login", http.MethodGet, "/api/v1/users/alice/loggedin", "", "bob", http.StatusForbidden, "users:admin"},
		{"check a good password", http.MethodPost, "/api/v1/auth/password/check", `{"password":"Long-enough"}`, "bob", http.StatusOK, `"valid":true`},
		{"check a bad password", http.MethodPost, "/api/v1/auth/password/check", `{"password":"short"}`, "bob", http.StatusOK, `"reason":"password shorter than 8 characters"`},
		{"check before a required change", http.MethodPost, "/api/v1/auth/password/check", `{"password":"Long-enough"}`, "fresh", http.StatusOK, `"valid":true`},
		{"check without a password", http.MethodPost, "/api/v1/auth/password/check", `{}`, "bob", http.StatusBadRequest, "password"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(s, tt.method, tt.path, tt.body, as(tt.user))
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("got %d %s; want %d containing %s", rec.Code, rec.Body.String(), tt.status, tt.want)
			}
		})
	}

	// A login token makes the user logged in; logging out ends it.
	token, err := s.sessions.create(Identity{Username: "alice"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rec := serve(s, http.MethodGet, "/api/v1/users/alice/loggedin", "", as("root")); !strings.Contains(rec.Body.String(), `"loggedIn":true,"sessions":1`) {
		t.Errorf("after a login: %s", rec.Body.String())
	}
	serve(s, http.MethodPost, "/api/v1/auth/logout", "", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) })
	if rec := serve(s, http.MethodGet, "/api/v1/users/alice/loggedin", "", as("root")); !strings.Contains(rec.Body.String(), `"loggedIn":false`) {
		t.Errorf("after the logout: %s", rec.Body.String())
	}

	// With user administration, an unknown user's status is 404.
	withUsers := New(Config{Users: fakeUsers{}})
	for path, status := range map[string]int{"/api/v1/users/u/loggedin": http.StatusOK, "/api/v1/users/ghost/loggedin": http.StatusNotFound} {
		if rec := serve(withUsers, http.MethodGet, path, "", nil); rec.Code != status {
			t.Errorf("GET %s = %d, want %d", path, rec.Code, status)
		}
	}

	// Without the backends: 503.
	bare := New(Config{})
	for _, path := range []string{"/api/v1/users/a/preferences"} {
		if rec := serve(bare, http.MethodGet, path, "", nil); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("GET %s = %d", path, rec.Code)
		}
	}
	if rec := serve(bare, http.MethodPost, "/api/v1/auth/password/check", `{"password":"x"}`, nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("password check = %d", rec.Code)
	}
}
