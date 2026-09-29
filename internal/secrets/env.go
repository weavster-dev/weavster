package secrets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

// Get first checks the process environment, then the file named key in
// the secrets directory, without one trailing newline (secret files often
// end with one). An empty variable counts as not set. A key with a path
// separator or ".." is never looked up. A file that exists but cannot be
// read is an error of its own, not ErrNotFound.
func (e *Env) Get(_ context.Context, key string) ([]byte, error) {
	if key == "" || key == "." || key == ".." || strings.ContainsAny(key, `/\`) {
		return nil, fmt.Errorf("%w: %q is not a secret name", ErrNotFound, key)
	}
	if v := os.Getenv(key); v != "" {
		return []byte(v), nil
	}
	b, err := os.ReadFile(filepath.Join(e.secretsDir, key))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("%w: %q", ErrNotFound, key)
	case err != nil:
		return nil, fmt.Errorf("secrets: %w", err)
	}
	b = bytes.TrimSuffix(b, []byte("\n"))
	return bytes.TrimSuffix(b, []byte("\r")), nil
}
