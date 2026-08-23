// Package secrets implements the SecretProvider port with local and env
// adapters; KMS/Vault rotation is an Enterprise interface-only extension.
package secrets

import (
	"context"
	"errors"
)

// ErrNotFound is returned when a secret key has no value.
var ErrNotFound = errors.New("secrets: not found")

// ErrEnterprise is returned by Enterprise-scoped adapters until a cloud
// provider is wired in (gap #8).
var ErrEnterprise = errors.New("secrets: enterprise feature not available")

// SecretProvider is the port for retrieving credential material (arch §3.1).
type SecretProvider interface {
	// Get returns the secret bytes for key.
	Get(ctx context.Context, key string) ([]byte, error)
}

// KeyManager is the Enterprise rotation port (cloud KMS/Vault). It exists in
// the MVP as an interface only; concrete rotation implementations are
// Enterprise-scoped (gap #8).
type KeyManager interface {
	Rotate(ctx context.Context, key string) error
}

// EnterpriseKeyManager is the Enterprise KMS/Vault adapter stub.
type EnterpriseKeyManager struct{}

// Rotate always returns ErrEnterprise.
func (EnterpriseKeyManager) Rotate(context.Context, string) error { return ErrEnterprise }

// Compile-time assertions that the MVP adapters satisfy the ports.
var (
	_ SecretProvider = (*Local)(nil)
	_ SecretProvider = (*Env)(nil)
	_ KeyManager     = EnterpriseKeyManager{}
)
