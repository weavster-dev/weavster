package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/auth"
	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/pipeline"
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

type failingSaveRepo struct{ userRepository }

func (failingSaveRepo) InsertUser(context.Context, state.UserDocument) error {
	return errors.New("attempt to write a readonly database")
}

func (failingSaveRepo) PutUser(context.Context, state.UserDocument) error {
	return errors.New("attempt to write a readonly database")
}

func TestBootstrapLosesRaceGracefully(t *testing.T) {
	ctx := context.Background()
	repo := state.NewMemStore()
	other := auth.NewLocalProvider(auth.Options{Store: userStoreAdapter{repo: repo}})
	if err := other.CreateUser(ctx, auth.User{Username: bootstrapAdmin, PasswordHash: "Other-Pass-1"}); err != nil {
		t.Fatal(err)
	}
	// This process saw an empty store before the other one inserted admin.
	p := auth.NewLocalProvider(auth.Options{Store: userStoreAdapter{repo: repo}})
	var out strings.Builder
	t.Setenv(envBootstrapPassword, "")
	if err := bootstrapAdminUser(ctx, p, auth.PasswordPolicy{}, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "password:") {
		t.Error("printed a password for an admin that was not created")
	}
	if _, err := p.Authenticate(ctx, bootstrapAdmin, "Other-Pass-1", ""); err != nil {
		t.Errorf("the other process's admin should be loaded: %v", err)
	}
}

func TestBootstrapReportsStorageFailure(t *testing.T) {
	p := auth.NewLocalProvider(auth.Options{Store: userStoreAdapter{repo: failingSaveRepo{}}})
	err := bootstrapAdminUser(context.Background(), p, auth.PasswordPolicy{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "bootstrap: saving the admin account") {
		t.Errorf("err = %v, want a storage error", err)
	}
}

func TestPasswordAdapterMapsErrors(t *testing.T) {
	ctx := context.Background()
	p := auth.NewLocalProvider(auth.Options{})
	if err := p.CreateUser(ctx, auth.User{Username: "u", PasswordHash: "Pass-1"}); err != nil {
		t.Fatal(err)
	}
	a := passwordAdapter{p}
	tests := []struct {
		name, user, old, new string
		want                 error
	}{
		{"wrong old", "u", "nope", "Pass-2", gateway.ErrWrongPassword},
		{"same password", "u", "Pass-1", "Pass-1", gateway.ErrPasswordRejected},
		{"unknown user", "ghost", "x", "y", auth.ErrUserNotFound},
		{"ok", "u", "Pass-1", "Pass-2", nil},
	}
	for _, tt := range tests {
		if err := a.ChangePassword(ctx, tt.user, tt.old, tt.new); !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.want)
		}
	}
}

// TestIngestSurvivesCancellation proves processing finishes even when the
// request context is cancelled, so no message is left half-processed.
func TestIngestSurvivesCancellation(t *testing.T) {
	store := state.NewMemStore()
	flows := flowAdapter{store: store}
	if err := flows.Create(context.Background(), gateway.Flow{ID: "f"}); err != nil {
		t.Fatal(err)
	}
	a := ingestAdapter{flows: flows, pipe: pipeline.New(store, newSink, nil, pipeline.Options{})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := a.Ingest(ctx, "f", []byte("x"))
	if err != nil || res.Status != "sent" {
		t.Errorf("Ingest with cancelled ctx = %+v, %v; want sent", res, err)
	}
}
