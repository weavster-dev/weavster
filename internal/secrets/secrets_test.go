package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

func TestEnterpriseKeyManagerStub(t *testing.T) {
	var km KeyManager = EnterpriseKeyManager{}
	if err := km.Rotate(context.Background(), "k"); err != ErrEnterprise {
		t.Errorf("expected ErrEnterprise, got %v", err)
	}
}

// TestEnvSecretNames: a trailing newline of a secret file is trimmed, and
// a name that is a path is never looked up.
func TestEnvSecretNames(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{"LF": "a\n", "CRLF": "b\r\n", "TWO": "c\n\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	e := NewEnv(filepath.Join(dir, "sub"))
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ key, want string }{{"../LF", ""}, {`..\LF`, ""}, {"..", ""}, {".", ""}, {"", ""}} {
		if got, err := e.Get(context.Background(), tt.key); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get(%q) = %q, %v", tt.key, got, err)
		}
	}
	e = NewEnv(dir)
	for key, want := range map[string]string{"LF": "a", "CRLF": "b", "TWO": "c\n"} {
		if got, err := e.Get(context.Background(), key); err != nil || string(got) != want {
			t.Errorf("Get(%s) = %q, %v; want %q", key, got, err, want)
		}
	}
}

// TestEnvFallbacks: an empty variable does not hide the file, and a file
// that cannot be read is an error, not ErrNotFound.
func TestEnvFallbacks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "WEAVSTER_EMPTY_ENV"), []byte("from-file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "A_DIRECTORY"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WEAVSTER_EMPTY_ENV", "")
	e := NewEnv(dir)
	if got, err := e.Get(context.Background(), "WEAVSTER_EMPTY_ENV"); err != nil || string(got) != "from-file" {
		t.Errorf("empty variable = %q, %v", got, err)
	}
	if _, err := e.Get(context.Background(), "A_DIRECTORY"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("unreadable = %v", err)
	}
}
