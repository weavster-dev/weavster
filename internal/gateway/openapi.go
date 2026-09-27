package gateway

import (
	"crypto/tls"
	_ "embed"
	"errors"
)

// openAPISpec is the served OpenAPI 3.1 contract (spec §5), the single
// source of truth: go generate copies it to agent-docs/openapi.yaml, and a
// test keeps the two identical.
//
//go:generate cp openapi.yaml ../../agent-docs/openapi.yaml
//go:embed openapi.yaml
var openAPISpec string

// OpenAPISpec returns the OpenAPI 3.1 contract.
func OpenAPISpec() string { return openAPISpec }

// ErrInvalidTLS is returned when a TLS configuration is unsatisfiable.
var ErrInvalidTLS = errors.New("gateway: invalid TLS configuration")

// TLSOptions configures the HTTPS listener (spec §2.13.44, §4.1).
type TLSOptions struct {
	MinVersion       uint16
	CipherSuites     []uint16
	CurvePreferences []tls.CurveID // ephemeral-DH group preference/sizing
}

// DefaultTLSOptions returns a hardened default: TLS 1.2+, strong AEAD ciphers,
// and modern ephemeral-DH curves.
func DefaultTLSOptions() TLSOptions {
	return TLSOptions{
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		},
		CurvePreferences: []tls.CurveID{tls.X25519, tls.CurveP256},
	}
}

// BuildTLSConfig returns a *tls.Config from the options (spec §2.13.44).
func BuildTLSConfig(opts TLSOptions) (*tls.Config, error) {
	if opts.MinVersion == 0 {
		return nil, ErrInvalidTLS
	}
	return &tls.Config{
		MinVersion:       opts.MinVersion,
		CipherSuites:     opts.CipherSuites,
		CurvePreferences: opts.CurvePreferences,
	}, nil
}
