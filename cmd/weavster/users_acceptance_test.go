package main

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/auth"
	"github.com/weavster-dev/weavster/internal/state"
)

// TestUsersSurviveRestart proves durable local users: the first-run admin is
// created once per data directory, and a changed password and an active
// lockout persist across restarts.
func TestUsersSurviveRestart(t *testing.T) {
	t.Setenv(envBootstrapPassword, "")
	addr := freeAddr(t)
	base := "http://" + addr
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n"+
		"auth: {lockout: {retryLimit: 2, lockoutPeriodSeconds: 600}}\n")
	args := []string{"server", "--config", cfg}
	c := apiClient{t: t, base: base}

	// First start: a generated password is printed; change it.
	stop, stderr := startCLIWithStderr(t, args, base+"/api/openapi.yaml")
	m := regexp.MustCompile(`password: (\S+)`).FindStringSubmatch(stderr.String())
	if m == nil {
		t.Fatalf("no generated password on first start: %q", stderr.String())
	}
	body := `{"oldPassword":"` + m[1] + `","newPassword":"Durable-Pass-9"}`
	if status, resp, _ := c.do(http.MethodPost, "/api/v1/auth/password", body, basic(bootstrapAdmin, m[1])); status != http.StatusNoContent {
		t.Fatalf("change password: %d %q", status, resp)
	}
	stop()

	// Second start: no new admin is created, and the changed password works.
	stop, stderr = startCLIWithStderr(t, args, base+"/api/openapi.yaml")
	if strings.Contains(stderr.String(), "First-run admin account created") {
		t.Error("bootstrap ran again although a user exists")
	}
	if status, _, _ := c.do(http.MethodGet, "/api/v1/flows", "", basic(bootstrapAdmin, "Durable-Pass-9")); status != http.StatusOK {
		t.Errorf("changed password after restart: %d, want 200", status)
	}
	if status, _, _ := c.do(http.MethodGet, "/api/v1/flows", "", basic(bootstrapAdmin, m[1])); status != http.StatusUnauthorized {
		t.Errorf("old password after restart: %d, want 401", status)
	}
	// Lock the account (the failure above counted as one strike).
	c.do(http.MethodGet, "/api/v1/flows", "", basic(bootstrapAdmin, "wrong"))
	stop()

	// Third start: the lockout is still in force.
	stop = startCLI(t, args, base+"/api/openapi.yaml")
	defer stop()
	if status, _, _ := c.do(http.MethodGet, "/api/v1/flows", "", basic(bootstrapAdmin, "Durable-Pass-9")); status != http.StatusUnauthorized {
		t.Errorf("locked account after restart: %d, want 401", status)
	}
}

func TestStoresImplementUserRepository(t *testing.T) {
	sqlite, err := state.OpenSQLite(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqlite.Close() }()
	for _, s := range []state.Store{state.NewMemStore(), sqlite} {
		if _, ok := s.(userRepository); !ok {
			t.Errorf("%T does not implement userRepository", s)
		}
	}
}

func TestUserStoreAdapter(t *testing.T) {
	ctx := context.Background()
	repo := state.NewMemStore()
	a := userStoreAdapter{repo: repo}
	if err := a.SaveUser(ctx, auth.User{Username: "u", Permissions: []string{"flows:view"}}); err != nil {
		t.Fatal(err)
	}
	users, err := a.LoadUsers(ctx)
	if err != nil || len(users) != 1 || users[0].Permissions[0] != "flows:view" {
		t.Errorf("LoadUsers = %+v, %v", users, err)
	}
	if err := a.DeleteUser(ctx, "u"); err != nil {
		t.Fatal(err)
	}
	if err := repo.PutUser(ctx, state.UserDocument{Username: "bad", Document: []byte("{")}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.LoadUsers(ctx); err == nil {
		t.Error("LoadUsers with a corrupt document: want error")
	}
	if _, err := (userStoreAdapter{repo: failingUserRepo{}}).LoadUsers(ctx); err == nil {
		t.Error("LoadUsers with failing repo: want error")
	}
}

type failingUserRepo struct{ userRepository }

func (failingUserRepo) ListUsers(context.Context) ([]state.UserDocument, error) {
	return nil, errors.New("store down")
}
