package gateway

import "net/http"

// Error codes of the JSON error envelope {"error":{"code","message"}}.
// Handlers use a specific code where one exists (UNAUTHORIZED,
// PASSWORD_CHANGE_REQUIRED, IMPORT_INCOMPLETE, ...), otherwise the code of
// the status.
var statusCodes = map[int]string{
	http.StatusBadRequest:            "BAD_REQUEST",
	http.StatusUnauthorized:          "UNAUTHORIZED",
	http.StatusForbidden:             "FORBIDDEN",
	http.StatusNotFound:              "NOT_FOUND",
	http.StatusMethodNotAllowed:      "METHOD_NOT_ALLOWED",
	http.StatusConflict:              "CONFLICT",
	http.StatusRequestEntityTooLarge: "PAYLOAD_TOO_LARGE",
	http.StatusInternalServerError:   "INTERNAL",
	http.StatusNotImplemented:        "NOT_IMPLEMENTED",
	http.StatusServiceUnavailable:    "SERVICE_UNAVAILABLE",
}

// writeError writes the JSON error envelope with a specific code.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeErrorWith(w, status, code, message, nil)
}

// writeErrorWith writes the error envelope plus extra top-level fields,
// such as what a multi-flow operation already wrote.
func writeErrorWith(w http.ResponseWriter, status int, code, message string, extra map[string]any) {
	body := map[string]any{"error": map[string]string{"code": code, "message": message}}
	for k, v := range extra {
		body[k] = v
	}
	writeJSON(w, status, body)
}

// writeStatusError writes the JSON error envelope with the status's code.
func writeStatusError(w http.ResponseWriter, status int, message string) {
	code, ok := statusCodes[status]
	if !ok {
		code = "ERROR"
	}
	writeError(w, status, code, message)
}
