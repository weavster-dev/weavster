package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/weavster-dev/weavster/internal/secrets"
)

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

// value returns the secret name; an empty one counts as not set.
func (r secretReader) value(ctx context.Context, name string) (string, error) {
	if b, err := r.p.Get(ctx, name); err == nil && len(b) > 0 {
		return string(b), nil
	}
	return "", fmt.Errorf("environment variable %s is not set, and there is no file %s", name, filepath.Join(r.dir, name))
}
