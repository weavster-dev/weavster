// Package enterprise holds the one sentinel error for Enterprise-deferred
// features (D-17): every stub returns ErrNotImplemented (wrapped to name
// the feature), and the API would answer it with 501 NOT_IMPLEMENTED. No
// endpoint of this edition reaches a stub today: Enterprise-only settings
// are refused as invalid (400) before that.
package enterprise

import "errors"

// ErrNotImplemented marks a feature that exists only in the Enterprise
// edition. Wrap it to name the feature: fmt.Errorf("%w: SSO", ErrNotImplemented).
var ErrNotImplemented = errors.New("not implemented in this edition")
