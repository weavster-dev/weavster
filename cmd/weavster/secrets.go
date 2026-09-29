package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/weavster-dev/weavster/internal/secrets"
)

// secretValues reads secrets by name (dbPool, portSources, the store).
type secretValues interface {
	value(ctx context.Context, name string) (string, error)
}

// secretReader reads the secrets that flows and the store name (dsnEnv,
// passwordEnv): the environment variable of that name or, when it is not
// set, the file of that name in secrets.dir (#107 D-98).
type secretReader struct {
	p   secrets.SecretProvider
	dir string
}

func newSecretReader(dir string) secretReader {
	return secretReader{p: secrets.NewEnv(dir), dir: dir}
}

// value returns the secret name; an empty one counts as not set. A file
// that exists but cannot be read (permissions) says so.
func (r secretReader) value(ctx context.Context, name string) (string, error) {
	b, err := r.p.Get(ctx, name)
	switch {
	case err == nil && len(b) > 0:
		return string(b), nil
	case err == nil || errors.Is(err, secrets.ErrNotFound):
		return "", fmt.Errorf("environment variable %s is not set, and there is no file %s", name, filepath.Join(r.dir, name))
	}
	return "", fmt.Errorf("secret %s: %w", name, err)
}
