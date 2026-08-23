package secrets

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Env reads secrets from environment variables and from a secrets directory
// (Docker "/run/secrets" convention). The directory is configurable for
// testability (gap #8).
type Env struct {
	secretsDir string
}

// NewEnv returns an env/file secret provider. An empty dir defaults to
// "/run/secrets".
func NewEnv(secretsDir string) *Env {
	if secretsDir == "" {
		secretsDir = "/run/secrets"
	}
	return &Env{secretsDir: secretsDir}
}

// Get first checks the process environment, then the secrets directory.
func (e *Env) Get(_ context.Context, key string) ([]byte, error) {
	if v, ok := os.LookupEnv(key); ok {
		return []byte(v), nil
	}
	if b, err := os.ReadFile(filepath.Join(e.secretsDir, key)); err == nil {
		return b, nil
	}
	return nil, fmt.Errorf("%w: %q", ErrNotFound, key)
}
