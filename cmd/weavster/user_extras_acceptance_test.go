package main

import (
	"net/http"
	"strings"
	"testing"
)

// TestUserExtras: a user keeps their own preferences (others need
// users:admin), their logged-in status follows login and logout, a
// candidate password is checked against the policy, and preferences
// survive a restart.
func TestUserExtras(t *testing.T) {
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\n"+storeConfig(t))
	args := []string{"server", "--config", cfg}
	stop := startCLI(t, args, "http://"+addr+"/api/openapi.yaml")
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	for _, u := range []string{"alice", "bob"} {
		if code, body, _ := c.do(http.MethodPost, "/api/v1/users",
			`{"username":"`+u+`","password":"User-Pass-1","permissions":["flows:view"],"mustChangePassword":false}`, admin); code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", u, code, body)
		}
	}
	alice, bob := basic("alice", "User-Pass-1"), basic("bob", "User-Pass-1")

	if code, body, _ := c.do(http.MethodPut, "/api/v1/users/alice/preferences", `{"theme":"dark","dashboard.flow":"adt"}`, alice); code != http.StatusOK {
		t.Fatalf("set preferences: %d %s", code, body)
	}
	for _, tt := range []struct {
		name   string
		creds  func(*http.Request)
		status int
		want   string
	}{
		{"alice reads hers", alice, http.StatusOK, `"theme":"dark"`},
		{"bob cannot", bob, http.StatusForbidden, "users:admin"},
		{"admin can", admin, http.StatusOK, `"dashboard.flow":"adt"`},
	} {
		if code, body, _ := c.do(http.MethodGet, "/api/v1/users/alice/preferences", "", tt.creds); code != tt.status || !strings.Contains(body, tt.want) {
			t.Errorf("%s: %d %s", tt.name, code, body)
		}
	}
	if code, body, _ := c.do(http.MethodPut, "/api/v1/users/alice/preferences", `{"":"x"}`, alice); code != http.StatusBadRequest {
		t.Errorf("an empty name: %d %s", code, body)
	}

	if _, body, _ := c.do(http.MethodGet, "/api/v1/users/alice/loggedin", "", admin); !strings.Contains(body, `"loggedIn":false`) {
		t.Errorf("before a login: %s", body)
	}
	code, token := c.login("alice", "User-Pass-1")
	if code != http.StatusOK {
		t.Fatalf("login: %d", code)
	}
	session := bearer(token)
	if _, body, _ := c.do(http.MethodGet, "/api/v1/users/alice/loggedin", "", session); !strings.Contains(body, `"loggedIn":true,"sessions":1`) {
		t.Errorf("after a login: %s", body)
	}
	c.do(http.MethodPost, "/api/v1/auth/logout", "", session)
	if _, body, _ := c.do(http.MethodGet, "/api/v1/users/alice/loggedin", "", admin); !strings.Contains(body, `"loggedIn":false`) {
		t.Errorf("after the logout: %s", body)
	}

	for pw, want := range map[string]string{"Good-Pass-1": `"valid":true`, "short": `"valid":false,"reason":"password shorter than 8 characters"`} {
		if _, body, _ := c.do(http.MethodPost, "/api/v1/auth/password/check", `{"password":"`+pw+`"}`, bob); !strings.Contains(body, want) {
			t.Errorf("check %q: %s, want %s", pw, body, want)
		}
	}

	stop()
	if !restartable(t) {
		return
	}
	stop = startCLI(t, args, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	if _, body, _ := c.do(http.MethodGet, "/api/v1/users/alice/preferences", "", alice); !strings.Contains(body, `"theme":"dark"`) {
		t.Errorf("preferences after a restart: %s", body)
	}
}
