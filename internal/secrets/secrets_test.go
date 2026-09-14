package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocal(t *testing.T) {
	ctx := context.Background()
	l := NewLocal()
	l.Set("db.password", []byte("hunter2"))

	got, err := l.Get(ctx, "db.password")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != "hunter2" {
		t.Errorf("got %q, want hunter2", got)
	}

	// The returned slice is a copy, not the stored backing array.
	got[0] = 'X'
	again, _ := l.Get(ctx, "db.password")
	if string(again) != "hunter2" {
		t.Errorf("store mutated by caller: %q", again)
	}

	if _, err := l.Get(ctx, "missing"); err == nil {
		t.Error("expected not-found error")
	}
}

func TestEnvFromEnvironment(t *testing.T) {
	t.Setenv("WEAVSTER_TEST_SECRET", "from-env")
	e := NewEnv(t.TempDir())
	got, err := e.Get(context.Background(), "WEAVSTER_TEST_SECRET")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != "from-env" {
		t.Errorf("got %q", got)
	}
}

func TestNewEnvDefaultsSecretsDirectory(t *testing.T) {
	e := NewEnv("")
	if got, want := e.secretsDir, "/run/secrets"; got != want {
		t.Errorf("NewEnv(\"\").secretsDir = %q, want %q", got, want)
	}
}

func TestEnvFromSecretsDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "smtp.pass"), []byte("s3cret"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := NewEnv(dir)
	got, err := e.Get(context.Background(), "smtp.pass")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != "s3cret" {
		t.Errorf("got %q", got)
	}
}

func TestEnvMissingSecretReturnsNotFound(t *testing.T) {
	const key = "WEAVSTER_TEST_SECRET_THAT_DOES_NOT_EXIST"
	old, present := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if present {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})

	got, err := NewEnv(t.TempDir()).Get(context.Background(), key)
	if got != nil {
		t.Errorf("Get returned bytes %q, want nil", got)
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get error = %v, want ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), key) {
		t.Errorf("Get error = %q, want requested key", err)
	}
}

func TestEnterpriseKeyManagerStub(t *testing.T) {
	var km KeyManager = EnterpriseKeyManager{}
	if err := km.Rotate(context.Background(), "k"); err != ErrEnterprise {
		t.Errorf("expected ErrEnterprise, got %v", err)
	}
}
