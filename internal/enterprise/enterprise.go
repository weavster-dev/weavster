// Package enterprise holds the one sentinel error for Enterprise-deferred
// features (D-17): every package returns ErrNotImplemented for them, the
// API answers 501 with code NOT_IMPLEMENTED, and the CLI shows that reply.
package enterprise

import "errors"

// ErrNotImplemented marks a feature that exists only in the Enterprise
// edition. Wrap it to name the feature: fmt.Errorf("%w: SSO", ErrNotImplemented).
var ErrNotImplemented = errors.New("not implemented in this edition")
