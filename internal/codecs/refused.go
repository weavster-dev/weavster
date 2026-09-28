package codecs

// RefusedError is input a JSON view refuses and why: Err is the format's
// sentinel (ErrNotXML, ErrNotDelimited) and Reason fixed words that never
// quote the input, since reasons reach events and replies.
type RefusedError struct {
	Err    error
	Reason string
}

func (e *RefusedError) Error() string {
	if e.Reason == "" {
		return e.Err.Error()
	}
	return e.Err.Error() + ": " + e.Reason
}

// Unwrap makes errors.Is(err, e.Err) hold.
func (e *RefusedError) Unwrap() error { return e.Err }
