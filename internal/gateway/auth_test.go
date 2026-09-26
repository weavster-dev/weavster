package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeAuth struct{ users map[string]Identity }

func (f fakeAuth) Authenticate(_ context.Context, user, pass, _ string) (Identity, error) {
	id, ok := f.users[user]
	if !ok || pass != "pw" {
		return Identity{}, errors.New("bad credentials")
	}
	return id, nil
}

type fakeAuthz struct{}

func (fakeAuthz) Authorize(_ context.Context, id Identity, resource, action string) bool {
	for _, p := range id.Permissions {
		if p == resource+":"+action {
			return true
		}
	}
	return false
}

type fakePasswords struct{ err error }

func (f fakePasswords) ChangePassword(context.Context, string, string, string) error { return f.err }

func newAuthServer(passwords PasswordChanger) *Server {
	return New(Config{
		Auth: fakeAuth{users: map[string]Identity{
			"viewer": {Username: "viewer", Permissions: []string{"flows:view"}},
			"fresh":  {Username: "fresh", MustChangePassword: true},
		}},
		Authorizer: fakeAuthz{},
		Passwords:  passwords,
		Flows:      &stubFlows{},
	})
}

type stubFlows struct{}

func (*stubFlows) List(context.Context) ([]Flow, error)           { return []Flow{}, nil }
func (*stubFlows) Get(context.Context, string) (Flow, error)      { return Flow{}, nil }
func (*stubFlows) Create(_ context.Context, f Flow) (Flow, error) { return f, nil }
func (*stubFlows) Delete(context.Context, string) error           { return nil }

func serve(s *Server, method, path, body string, set func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if set != nil {
		set(req)
	}
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	return rec
}

func TestAuthMiddleware(t *testing.T) {
	s := newAuthServer(fakePasswords{})
	token, err := s.sessions.create(Identity{Username: "viewer", Permissions: []string{"flows:view"}})
	if err != nil {
		t.Fatal(err)
	}
	expired, _ := s.sessions.create(Identity{Username: "viewer"})
	s.sessions.tokens[expired] = session{expires: time.Now().Add(-time.Minute)}

	tests := []struct {
		name   string
		method string
		path   string
		set    func(*http.Request)
		want   int
		body   string
	}{
		{"no credentials", http.MethodGet, "/api/v1/flows", nil, http.StatusUnauthorized, "UNAUTHORIZED"},
		{"wrong basic", http.MethodGet, "/api/v1/flows", func(r *http.Request) { r.SetBasicAuth("viewer", "no") }, http.StatusUnauthorized, ""},
		{"basic ok", http.MethodGet, "/api/v1/flows", func(r *http.Request) { r.SetBasicAuth("viewer", "pw") }, http.StatusOK, ""},
		{"bearer ok", http.MethodGet, "/api/v1/flows", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }, http.StatusOK, ""},
		{"bearer lowercase scheme", http.MethodGet, "/api/v1/flows", func(r *http.Request) { r.Header.Set("Authorization", "bearer "+token) }, http.StatusOK, ""},
		{"bearer expired", http.MethodGet, "/api/v1/flows", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+expired) }, http.StatusUnauthorized, ""},
		{"forbidden", http.MethodPost, "/api/v1/flows", func(r *http.Request) { r.SetBasicAuth("viewer", "pw") }, http.StatusForbidden, "missing permission flows:edit"},
		{"must change blocks", http.MethodGet, "/api/v1/system", func(r *http.Request) { r.SetBasicAuth("fresh", "pw") }, http.StatusForbidden, "PASSWORD_CHANGE_REQUIRED"},
		{"must change allows me", http.MethodGet, "/api/v1/auth/me", func(r *http.Request) { r.SetBasicAuth("fresh", "pw") }, http.StatusOK, `"mustChangePassword":true`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(s, tt.method, tt.path, "", tt.set)
			if rec.Code != tt.want || !strings.Contains(rec.Body.String(), tt.body) {
				t.Errorf("got %d %q, want %d containing %q", rec.Code, rec.Body.String(), tt.want, tt.body)
			}
		})
	}
}

func TestMissingAuthorizerFailsClosed(t *testing.T) {
	s := New(Config{Auth: fakeAuth{users: map[string]Identity{"viewer": {Username: "viewer"}}}, Flows: &stubFlows{}})
	if rec := serve(s, http.MethodGet, "/api/v1/flows", "", func(r *http.Request) { r.SetBasicAuth("viewer", "pw") }); rec.Code != http.StatusForbidden {
		t.Errorf("no authorizer: %d, want 403", rec.Code)
	}
}

func TestAuthDisabledWithoutProvider(t *testing.T) {
	s := New(Config{Flows: &stubFlows{}})
	if rec := serve(s, http.MethodPost, "/api/v1/flows", `{"id":"a"}`, nil); rec.Code != http.StatusCreated {
		t.Errorf("no provider: %d, want 201", rec.Code)
	}
	if rec := serve(s, http.MethodPost, "/api/v1/auth/login", `{}`, nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("login without provider: %d, want 503", rec.Code)
	}
}

func TestLoginAndPasswordHandlers(t *testing.T) {
	tests := []struct {
		name      string
		passwords PasswordChanger
		path      string
		body      string
		set       func(*http.Request)
		want      int
	}{
		{"login bad json", fakePasswords{}, "/api/v1/auth/login", "x", nil, http.StatusBadRequest},
		{"login wrong", fakePasswords{}, "/api/v1/auth/login", `{"username":"viewer","password":"no"}`, nil, http.StatusUnauthorized},
		{"login ok", fakePasswords{}, "/api/v1/auth/login", `{"username":"viewer","password":"pw"}`, nil, http.StatusOK},
		{"change unavailable", nil, "/api/v1/auth/password", `{}`, func(r *http.Request) { r.SetBasicAuth("fresh", "pw") }, http.StatusServiceUnavailable},
		{"change bad json", fakePasswords{}, "/api/v1/auth/password", "x", func(r *http.Request) { r.SetBasicAuth("fresh", "pw") }, http.StatusBadRequest},
		{"change rejected", fakePasswords{err: fmt.Errorf("%w: too short", ErrPasswordRejected)}, "/api/v1/auth/password", `{}`, func(r *http.Request) { r.SetBasicAuth("fresh", "pw") }, http.StatusBadRequest},
		{"change internal failure", fakePasswords{err: errors.New("database is locked")}, "/api/v1/auth/password", `{}`, func(r *http.Request) { r.SetBasicAuth("fresh", "pw") }, http.StatusInternalServerError},
		{"change wrong old", fakePasswords{err: ErrWrongPassword}, "/api/v1/auth/password", `{}`, func(r *http.Request) { r.SetBasicAuth("fresh", "pw") }, http.StatusBadRequest},
		{"change ok basic", fakePasswords{}, "/api/v1/auth/password", `{}`, func(r *http.Request) { r.SetBasicAuth("fresh", "pw") }, http.StatusNoContent},
		{"logout", fakePasswords{}, "/api/v1/auth/logout", "", func(r *http.Request) { r.SetBasicAuth("viewer", "pw") }, http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(newAuthServer(tt.passwords), http.MethodPost, tt.path, tt.body, tt.set)
			if rec.Code != tt.want {
				t.Errorf("got %d %q, want %d", rec.Code, rec.Body.String(), tt.want)
			}
		})
	}
}

func TestChangePasswordClearsSessionFlag(t *testing.T) {
	s := newAuthServer(fakePasswords{})
	token, _ := s.sessions.create(Identity{Username: "fresh", MustChangePassword: true})
	withToken := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
	if rec := serve(s, http.MethodGet, "/api/v1/system", "", withToken); rec.Code != http.StatusForbidden {
		t.Fatalf("before change: %d, want 403", rec.Code)
	}
	if rec := serve(s, http.MethodPost, "/api/v1/auth/password", `{}`, withToken); rec.Code != http.StatusNoContent {
		t.Fatalf("change: %d", rec.Code)
	}
	if rec := serve(s, http.MethodGet, "/api/v1/system", "", withToken); rec.Code != http.StatusOK {
		t.Errorf("after change: %d, want 200", rec.Code)
	}
	s.sessions.update("unknown", Identity{}) // no-op for an unknown token
	if rec := serve(s, http.MethodPost, "/api/v1/auth/logout", "", withToken); rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", rec.Code)
	}
	if rec := serve(s, http.MethodGet, "/api/v1/system", "", withToken); rec.Code != http.StatusUnauthorized {
		t.Errorf("after logout: %d, want 401", rec.Code)
	}
}

func TestSessionsSweepAndRevokeUser(t *testing.T) {
	s := newSessions()
	old, _ := s.create(Identity{Username: "a"})
	s.tokens[old] = session{id: Identity{Username: "a"}, expires: time.Now().Add(-time.Second)}
	keep, _ := s.create(Identity{Username: "a"})
	if _, ok := s.tokens[old]; ok {
		t.Error("expired session not swept on create")
	}
	drop, _ := s.create(Identity{Username: "a"})
	other, _ := s.create(Identity{Username: "b"})
	s.revokeUser("a", keep)
	for token, want := range map[string]bool{keep: true, drop: false, other: true} {
		if _, ok := s.lookup(token); ok != want {
			t.Errorf("token present = %v, want %v", ok, want)
		}
	}
}

func TestIdentityFrom(t *testing.T) {
	if _, ok := IdentityFrom(context.Background()); ok {
		t.Error("empty context returned an identity")
	}
	ctx := context.WithValue(context.Background(), identityKey{}, Identity{Username: "u"})
	if id, ok := IdentityFrom(ctx); !ok || id.Username != "u" {
		t.Errorf("IdentityFrom = %+v, %v", id, ok)
	}
}
