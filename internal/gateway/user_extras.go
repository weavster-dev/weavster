package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// UserPreferences reads and replaces a user's preferences (spec §5).
type UserPreferences interface {
	// Preferences returns the user's preferences (ErrUserNotFound).
	Preferences(ctx context.Context, username string) (map[string]string, error)
	// SetPreferences replaces them (ErrUserNotFound). Unless asAdmin, it
	// refuses an account that has admin (ErrAdminTarget).
	SetPreferences(ctx context.Context, username string, prefs map[string]string, asAdmin bool) error
}

// PasswordChecker checks a candidate password against the password policy.
type PasswordChecker interface {
	CheckPassword(password string) error
}

// Preference limits.
const (
	maxPreferences     = 100
	maxPreferenceKey   = 100
	maxPreferenceValue = 4096
)

// selfOrAdmin reports whether the caller may act for name: they are name,
// or they have users:admin (without authentication, anyone may).
func (s *Server) selfOrAdmin(r *http.Request, name string) bool {
	if s.cfg.Auth == nil {
		return true
	}
	id, _ := IdentityFrom(r.Context())
	return id.Username == name || (s.cfg.Authorizer != nil && s.cfg.Authorizer.Authorize(r.Context(), id, "users", "admin"))
}

func (s *Server) handlePreferencesGet(w http.ResponseWriter, r *http.Request) {
	name, ok := s.preferencesFor(w, r)
	if !ok {
		return
	}
	prefs, err := s.cfg.Preferences.Preferences(r.Context(), name)
	if err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

func (s *Server) handlePreferencesPut(w http.ResponseWriter, r *http.Request) {
	name, ok := s.preferencesFor(w, r)
	if !ok {
		return
	}
	var prefs map[string]string
	if !decodeStrict(w, r, &prefs) {
		return
	}
	if prefs == nil {
		writeStatusError(w, http.StatusBadRequest, "body must be a JSON object of text values")
		return
	}
	if msg := checkPreferences(prefs); msg != "" {
		writeStatusError(w, http.StatusBadRequest, msg)
		return
	}
	// Your own preferences are yours to change; another's follow the
	// same admin rule as the other user changes.
	self := false
	if id, ok := IdentityFrom(r.Context()); ok {
		self = id.Username == name
	}
	if err := s.cfg.Preferences.SetPreferences(r.Context(), name, prefs, self || asAdmin(r)); err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

// preferencesFor checks the preferences routes' preconditions and returns
// the user they are for.
func (s *Server) preferencesFor(w http.ResponseWriter, r *http.Request) (string, bool) {
	if s.cfg.Preferences == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "preferences unavailable")
		return "", false
	}
	name := r.PathValue("name")
	if !s.selfOrAdmin(r, name) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "only the user themselves or users:admin")
		return "", false
	}
	return name, true
}

// checkPreferences says what is wrong with prefs, or "".
func checkPreferences(prefs map[string]string) string {
	if len(prefs) > maxPreferences {
		return fmt.Sprintf("at most %d preferences", maxPreferences)
	}
	for k, v := range prefs {
		if n := utf8.RuneCountInString(k); n == 0 || n > maxPreferenceKey || strings.ContainsRune(k, 0) {
			return fmt.Sprintf("preference names must be 1-%d characters of text", maxPreferenceKey)
		}
		if len(v) > maxPreferenceValue || strings.ContainsRune(v, 0) {
			return fmt.Sprintf("the value of %q must be text of at most %d bytes", k, maxPreferenceValue)
		}
	}
	return ""
}

// loggedInResponse is the reply of GET /api/v1/users/{name}/loggedin.
type loggedInResponse struct {
	LoggedIn bool `json:"loggedIn"`
	Sessions int  `json:"sessions"`
}

func (s *Server) handleLoggedIn(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Users == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "user administration unavailable")
		return
	}
	name := r.PathValue("name")
	if !s.selfOrAdmin(r, name) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "only the user themselves or users:admin")
		return
	}
	if _, err := s.cfg.Users.GetUser(r.Context(), name); err != nil {
		writeUserError(w, err)
		return
	}
	n := s.sessions.active(name)
	writeJSON(w, http.StatusOK, loggedInResponse{LoggedIn: n > 0, Sessions: n})
}

// active counts username's login tokens that are still valid.
func (s *sessions) active(username string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now, n := time.Now(), 0
	for _, sess := range s.tokens {
		if sess.id.Username == username && now.Before(sess.expires) && sess.authAt.After(s.revokedAt[username]) {
			n++
		}
	}
	return n
}

// passwordCheckResponse is the reply of POST /api/v1/auth/password/check.
type passwordCheckResponse struct {
	Valid  bool   `json:"valid"`
	Reason string `json:"reason,omitempty"`
}

func (s *Server) handlePasswordCheck(w http.ResponseWriter, r *http.Request) {
	if s.cfg.PasswordCheck == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "password check unavailable")
		return
	}
	var req struct {
		Password *string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil || req.Password == nil {
		writeStatusError(w, http.StatusBadRequest, "body must be JSON with password")
		return
	}
	if err := s.cfg.PasswordCheck.CheckPassword(*req.Password); err != nil {
		writeJSON(w, http.StatusOK, passwordCheckResponse{Reason: strings.TrimPrefix(err.Error(), "auth: ")})
		return
	}
	writeJSON(w, http.StatusOK, passwordCheckResponse{Valid: true})
}
