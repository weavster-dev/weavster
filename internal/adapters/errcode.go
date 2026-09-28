package adapters

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"syscall"
)

// A coded error carries the protocol-specific code of a delivery failure
// (#107 D-78): "http:503", "mllp:AE", "sqlstate:23505", "net:timeout", …
type coded interface{ Code() string }

// codedError is an error with a code; errors.Is/As see the wrapped error.
type codedError struct {
	code string
	err  error
}

func (e *codedError) Error() string { return e.err.Error() }
func (e *codedError) Unwrap() error { return e.err }
func (e *codedError) Code() string  { return e.code }

// withCode wraps err with code.
func withCode(code string, err error) error { return &codedError{code: code, err: err} }

// WithCode wraps err with a protocol-specific code for its attempt record
// (for sinks outside this package).
func WithCode(code string, err error) error { return withCode(code, err) }

// ErrorCode is err's protocol-specific code: the code a sink attached, or a
// network or TLS failure's kind ("net:timeout", "net:refused",
// "net:reset", "net:dns", "tls:certificate"); "" when it has none.
func ErrorCode(err error) string {
	var c coded
	if errors.As(err, &c) {
		return c.Code()
	}
	var dns *net.DNSError
	var ne net.Error
	var unknownCA x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var host x509.HostnameError
	var verify *tls.CertificateVerificationError
	switch {
	case err == nil:
		return ""
	case errors.As(err, &verify), errors.As(err, &unknownCA), errors.As(err, &invalid), errors.As(err, &host):
		return "tls:certificate"
	case errors.As(err, &dns):
		return "net:dns"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "net:refused"
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE):
		return "net:reset"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return "net:timeout"
	}
	return ""
}
