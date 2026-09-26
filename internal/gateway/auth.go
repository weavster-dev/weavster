package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SessionTTL is how long a login token stays valid.
const SessionTTL = 12 * time.Hour

// ErrWrongPassword is returned by a PasswordChanger when the old password
// does not match.
var ErrWrongPassword = errors.New("old password is incorrect")

// PasswordChanger changes a user's password (spec §5 password change).
type PasswordChanger interface {
	ChangePassword(ctx context.Context, username, oldPassword, newPassword string) error
}

type identityKey struct{}

// IdentityFrom returns the authenticated identity stored on ctx by the
// authentication middleware.
func IdentityFrom(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(identityKey{}).(Identity)
	return id, ok
}

type session struct {
	id      Identity
	expires time.Time
}

// sessions is the in-memory login token table.
type sessions struct {
	mu     sync.Mutex
	tokens map[string]session
}

func newSessions() *sessions { return &sessions{tokens: map[string]session{}} }

func (s *sessions) create(id Identity) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for t, sess := range s.tokens { // sweep expired sessions
		if now.After(sess.expires) {
			delete(s.tokens, t)
		}
	}
	s.tokens[token] = session{id: id, expires: now.Add(SessionTTL)}
	return token, nil
}

func (s *sessions) lookup(token string) (Identity, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.tokens[token]
	if !ok || time.Now().After(sess.expires) {
		delete(s.tokens, token)
		return Identity{}, false
	}
	return sess.id, true
}

func (s *sessions) update(token string, id Identity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.tokens[token]; ok {
		sess.id = id
		s.tokens[token] = sess
	}
}

// revokeUser revokes every session of username except keep.
func (s *sessions) revokeUser(username, keep string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for t, sess := range s.tokens {
		if t != keep && sess.id.Username == username {
			delete(s.tokens, t)
		}
	}
}

func (s *sessions) revoke(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, token)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// bearerToken returns the token of a Bearer Authorization header. The scheme
// name is case-insensitive (RFC 7235).
func bearerToken(r *http.Request) string {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

// authenticate resolves a Bearer token or Basic credentials into an Identity.
// Requests without valid credentials get 401. With no AuthProvider
// configured, authentication is disabled.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Auth == nil {
			next.ServeHTTP(w, r)
			return
		}
		var (
			id Identity
			ok bool
		)
		if token := bearerToken(r); token != "" {
			id, ok = s.sessions.lookup(token)
		} else if user, pass, basic := r.BasicAuth(); basic {
			var err error
			id, err = s.cfg.Auth.Authenticate(r.Context(), user, pass, r.Header.Get("X-Weavster-MFA"))
			ok = err == nil
		}
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="weavster"`)
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
			return
		}
		if id.MustChangePassword && r.URL.Path != "/api/v1/auth/password" &&
			r.URL.Path != "/api/v1/auth/logout" && r.URL.Path != "/api/v1/auth/me" {
			writeError(w, http.StatusForbidden, "PASSWORD_CHANGE_REQUIRED",
				"change your password with POST /api/v1/auth/password before using the API")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
	})
}

// require returns middleware that allows the request only when the
// Authorizer grants resource:action to the authenticated identity.
func (s *Server) require(resource, action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s.cfg.Auth != nil && s.cfg.Authorizer != nil {
				id, _ := IdentityFrom(r.Context())
				if !s.cfg.Authorizer.Authorize(r.Context(), id, resource, action) {
					writeError(w, http.StatusForbidden, "FORBIDDEN", "missing permission "+resource+":"+action)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	MFACode  string `json:"mfaCode"`
}

type identityResponse struct {
	Username           string   `json:"username"`
	Permissions        []string `json:"permissions"`
	MustChangePassword bool     `json:"mustChangePassword"`
}

func toIdentityResponse(id Identity) identityResponse {
	perms := id.Permissions
	if perms == nil {
		perms = []string{}
	}
	return identityResponse{Username: id.Username, Permissions: perms, MustChangePassword: id.MustChangePassword}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Auth == nil {
		writeError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "authentication is not configured")
		return
	}
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "body must be JSON with username and password")
		return
	}
	id, err := s.cfg.Auth.Authenticate(r.Context(), req.Username, req.Password, req.MFACode)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid username or password")
		return
	}
	token, err := s.sessions.create(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "could not create session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":     token,
		"expiresAt": time.Now().Add(SessionTTL).UTC().Format(time.RFC3339),
		"user":      toIdentityResponse(id),
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.sessions.revoke(bearerToken(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	id, _ := IdentityFrom(r.Context())
	writeJSON(w, http.StatusOK, toIdentityResponse(id))
}

type passwordRequest struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Passwords == nil {
		writeError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "password change is not configured")
		return
	}
	id, _ := IdentityFrom(r.Context())
	var req passwordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "body must be JSON with oldPassword and newPassword")
		return
	}
	if err := s.cfg.Passwords.ChangePassword(r.Context(), id.Username, req.OldPassword, req.NewPassword); err != nil {
		if errors.Is(err, ErrWrongPassword) {
			writeError(w, http.StatusBadRequest, "OLD_PASSWORD_INCORRECT", "oldPassword is incorrect")
			return
		}
		writeError(w, http.StatusBadRequest, "PASSWORD_REJECTED", err.Error())
		return
	}
	// Other sessions carry the old credentials' identity: revoke them, and
	// keep only the token used for this change.
	token := bearerToken(r)
	s.sessions.revokeUser(id.Username, token)
	id.MustChangePassword = false
	if token != "" {
		s.sessions.update(token, id)
	}
	w.WriteHeader(http.StatusNoContent)
}
