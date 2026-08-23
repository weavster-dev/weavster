package auth

import "errors"

// GenericFailureMessage is the identical login-failure message returned for
// every failure when anti-enumeration is enabled (spec §2.13.43). It must not
// reveal username existence or lockout state.
const GenericFailureMessage = "invalid username or password"

// ErrGenericFailure is returned for every authentication failure when
// anti-enumeration is enabled.
var ErrGenericFailure = errors.New(GenericFailureMessage)
