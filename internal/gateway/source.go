package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
)

// SourceIngester runs a message a flow's own source received through the
// flow, storing metadata about where it came from.
type SourceIngester interface {
	IngestFrom(ctx context.Context, flowID string, body []byte, metadata map[string]string) (IngestResult, error)
}

// SourceHandler serves a flow's http source (#107 D-57): a request with the
// source's method and path runs its body through the flow like POST
// /api/v1/flows/{id}/messages, with the same limit and reply.
func SourceHandler(flowID string, src FlowSource, ingest SourceIngester) http.Handler {
	path, method := src.Path, src.Method
	if path == "" {
		path = "/"
	}
	if method == "" {
		method = http.MethodPost
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			writeStatusError(w, http.StatusNotFound, "no source at this path")
			return
		}
		if r.Method != method {
			w.Header().Set("Allow", method)
			writeStatusError(w, http.StatusMethodNotAllowed, "use "+method)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxMessageBytes))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeStatusError(w, http.StatusRequestEntityTooLarge, "message body larger than 10 MiB")
				return
			}
			writeStatusError(w, http.StatusBadRequest, "could not read message body")
			return
		}
		res, err := ingest.IngestFrom(r.Context(), flowID, body, map[string]string{"source.http.path": r.URL.Path})
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
