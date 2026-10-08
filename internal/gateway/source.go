package gateway

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
)

// SourceIngester runs a message a flow's own source received through the
// flow, storing metadata about where it came from.
type SourceIngester interface {
	IngestFrom(ctx context.Context, flowID string, body []byte, metadata map[string]string) (IngestResult, error)
}

// SourceHandler serves a flow's http source (#107 D-57): a request with the
// source's method and path runs its body through the flow like POST
// /api/v1/flows/{id}/messages, with the same limit and reply. Its errors are
// for a sending system rather than an API client: a flow that is not
// running is 503 (try again later), and a message stored before a later
// failure is 202, because the flow has it and a resend would duplicate it.
// With src.Username set, every request must carry that user and password
// (HTTP Basic), checked before anything else.
func SourceHandler(flowID string, src FlowSource, password string, ingest SourceIngester) http.Handler {
	path, method := src.Path, src.Method
	if path == "" {
		path = "/"
	}
	if method == "" {
		method = http.MethodPost
	}
	wantUser, wantPassword := sha256.Sum256([]byte(src.Username)), sha256.Sum256([]byte(password))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if src.Username != "" {
			user, pw, _ := r.BasicAuth()
			gotUser, gotPassword := sha256.Sum256([]byte(user)), sha256.Sum256([]byte(pw))
			// Both compared, in constant time, whatever the first gives.
			userOK := subtle.ConstantTimeCompare(gotUser[:], wantUser[:])
			passwordOK := subtle.ConstantTimeCompare(gotPassword[:], wantPassword[:])
			if userOK&passwordOK != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="weavster", charset="UTF-8"`)
				writeStatusError(w, http.StatusUnauthorized, "authentication required")
				return
			}
		}
		if r.URL.Path != path {
			writeStatusError(w, http.StatusNotFound, "no source at this path")
			return
		}
		if r.Method != method {
			w.Header().Set("Allow", method)
			writeStatusError(w, http.StatusMethodNotAllowed, "use "+method)
			return
		}
		body, ok := readMessage(w, r)
		if !ok {
			return
		}
		// The request is fully read: a read deadline passing while the flow
		// processes it must not cancel the work.
		res, err := ingest.IngestFrom(context.WithoutCancel(r.Context()), flowID, body, map[string]string{"source.http.path": r.URL.Path})
		switch {
		case err == nil, res.ID != "":
			// A message stored before a later failure is accepted: the
			// flow owns it now, and a resend would duplicate it.
			writeJSON(w, http.StatusAccepted, res)
		case errors.Is(err, ErrFlowNotRunning), errors.Is(err, ErrFlowNotFound):
			writeStatusError(w, http.StatusServiceUnavailable, "flow is not accepting messages")
		case errors.Is(err, ErrInvalidMessage):
			writeStatusError(w, http.StatusBadRequest, err.Error())
		default:
			writeBackendError(w, err)
		}
	})
}
