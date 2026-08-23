package gateway

import "net/http"

// MarkerHeader is the cross-site-request marker header required on API calls
// (spec §2.13.45, §10). Its value must equal MarkerValue.
const (
	MarkerHeader = "X-Weavster-CSRF"
	MarkerValue  = "1"
)

// SecurityHeaders emits transport-hardening headers: HSTS, clickjacking
// (X-Frame-Options DENY + CSP frame-ancestors 'none'), and content-type
// sniffing protection (spec §10).
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

// RequireMarkerHeader enforces the CSRF marker header, returning HTTP 400
// when it is absent or wrong (spec §2.13.45).
func RequireMarkerHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(MarkerHeader) != MarkerValue {
			http.Error(w, "missing cross-site-request marker header", http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// BlockTrace rejects TRACE/TRACK with HTTP 405 (spec §10).
func BlockTrace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodTrace || r.Method == "TRACK" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}
