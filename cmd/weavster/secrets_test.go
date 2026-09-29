package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// noSecrets reads secrets from the environment only (its directory does
// not exist).
var noSecrets = newSecretReader("/nonexistent/weavster-secrets")

// TestSecretReader: a secret is the environment variable or else the file
// in the secrets directory; a missing or empty one names both places.
func TestSecretReader(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "WEAVSTER_DB_FILE"), []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "WEAVSTER_DB_EMPTY"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WEAVSTER_DB_BOTH", "from-env")
	if err := os.WriteFile(filepath.Join(dir, "WEAVSTER_DB_BOTH"), []byte("from-file"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newSecretReader(dir)
	for _, tt := range []struct{ name, want, err string }{
		{"WEAVSTER_DB_FILE", "from-file", ""},
		{"WEAVSTER_DB_BOTH", "from-env", ""},
		{"WEAVSTER_DB_EMPTY", "", "environment variable WEAVSTER_DB_EMPTY is not set, and there is no file " + filepath.Join(dir, "WEAVSTER_DB_EMPTY")},
		{"WEAVSTER_DB_NONE", "", "WEAVSTER_DB_NONE is not set"},
	} {
		got, err := r.value(ctx, tt.name)
		if got != tt.want || (err == nil) != (tt.err == "") || (err != nil && !strings.Contains(err.Error(), tt.err)) {
			t.Errorf("%s = %q, %v", tt.name, got, err)
		}
	}
}

// TestOpenPostgresSecret: a store.dsnEnv secret that is missing stops the
// connection with the secret's name.
func TestOpenPostgresSecret(t *testing.T) {
	_, err := openPostgres(context.Background(), serverconfig.Store{Dialect: serverconfig.DialectPostgres, DSNEnv: "WEAVSTER_STORE_NONE", MaxConnections: 1}, noSecrets)
	if err == nil || !strings.Contains(err.Error(), "store.dsnEnv: environment variable WEAVSTER_STORE_NONE is not set") {
		t.Errorf("err = %v", err)
	}
}
