package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"unicode"
	"unicode/utf8"

	"github.com/weavster-dev/weavster/internal/artifact"
)

// LookupStore keeps dynamic lookups: groups of string key → string value
// (spec §5).
type LookupStore interface {
	LookupGroups(ctx context.Context) (map[string]int, error)
	// LookupEntries returns the entries whose key starts with prefix, the
	// first limit in key order (0 = all).
	LookupEntries(ctx context.Context, group, prefix string, limit int) (map[string]string, error)
	LookupGet(ctx context.Context, group string, keys []string) (map[string]string, error)
	// LookupPut creates or replaces entries, all or nothing; with replace the
	// group holds exactly entries afterwards.
	LookupPut(ctx context.Context, group string, entries map[string]string, replace bool) error
	LookupDelete(ctx context.Context, group, key string) error // ErrLookupNotFound
	LookupDeleteGroup(ctx context.Context, group string) error // ErrLookupNotFound
}

// ErrLookupNotFound: no such lookup key, or a group without entries.
var ErrLookupNotFound = errors.New("lookup not found")

// Lookup limits.
const (
	maxLookupKey    = 512      // characters
	maxLookupValue  = 64 << 10 // bytes
	maxLookupBatch  = 1000     // keys per batch
	maxLookupLimit  = 10000    // entries per matching request
	defaultLookupLn = 1000     // entries per matching request by default
)

// checkLookupKey validates a key: 1–512 characters, no control characters.
func checkLookupKey(k string) error {
	if n := utf8.RuneCountInString(k); n == 0 || n > maxLookupKey || !utf8.ValidString(k) {
		return fmt.Errorf("key %q must be 1-%d characters", k, maxLookupKey)
	}
	for _, r := range k {
		if unicode.IsControl(r) {
			return fmt.Errorf("key %q must not contain control characters", k)
		}
	}
	return nil
}

func checkLookupValue(k, v string) error {
	if len(v) > maxLookupValue || !utf8.ValidString(v) {
		return fmt.Errorf("value of %q must be UTF-8 text of at most 64 KiB", k)
	}
	return nil
}

func (s *Server) lookupsAvailable(w http.ResponseWriter) bool {
	if s.cfg.Lookups == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "lookups unavailable")
		return false
	}
	return true
}

// lookupGroup reads and validates {group}; on failure it answers 400.
func lookupGroup(w http.ResponseWriter, r *http.Request) (string, bool) {
	g := r.PathValue("group")
	if err := artifact.CheckName(g); err != nil {
		writeStatusError(w, http.StatusBadRequest, "group: "+err.Error())
		return "", false
	}
	return g, true
}

// lookupKey reads and validates {group} and {key}.
func lookupKey(w http.ResponseWriter, r *http.Request) (group, key string, ok bool) {
	if group, ok = lookupGroup(w, r); !ok {
		return "", "", false
	}
	key = r.PathValue("key")
	if err := checkLookupKey(key); err != nil {
		writeStatusError(w, http.StatusBadRequest, err.Error())
		return "", "", false
	}
	return group, key, true
}

func writeLookupError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrLookupNotFound) {
		writeStatusError(w, http.StatusNotFound, "lookup not found")
		return
	}
	writeBackendError(w, err)
}

// LookupGroupInfo is one group in the list of groups.
type LookupGroupInfo struct {
	Name    string `json:"name"`
	Entries int    `json:"entries"`
}

func (s *Server) handleLookupGroups(w http.ResponseWriter, r *http.Request) {
	if !s.lookupsAvailable(w) {
		return
	}
	groups, err := s.cfg.Lookups.LookupGroups(r.Context())
	if err != nil {
		writeLookupError(w, err)
		return
	}
	out := make([]LookupGroupInfo, 0, len(groups))
	for g, n := range groups {
		out = append(out, LookupGroupInfo{Name: g, Entries: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, out)
}

// handleLookupMatching returns a group's entries, optionally by key prefix.
func (s *Server) handleLookupMatching(w http.ResponseWriter, r *http.Request) {
	if !s.lookupsAvailable(w) {
		return
	}
	group, ok := lookupGroup(w, r)
	if !ok {
		return
	}
	limit := defaultLookupLn
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxLookupLimit {
			writeStatusError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", maxLookupLimit))
			return
		}
		limit = n
	}
	entries, err := s.cfg.Lookups.LookupEntries(r.Context(), group, r.URL.Query().Get("prefix"), limit)
	if err != nil {
		writeLookupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleLookupDeleteGroup(w http.ResponseWriter, r *http.Request) {
	if !s.lookupsAvailable(w) {
		return
	}
	group, ok := lookupGroup(w, r)
	if !ok {
		return
	}
	if err := s.cfg.Lookups.LookupDeleteGroup(r.Context(), group); err != nil {
		writeLookupError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleLookupImport stores a JSON object of key: value, all or nothing;
// replace=true makes it the whole group.
func (s *Server) handleLookupImport(w http.ResponseWriter, r *http.Request) {
	if !s.lookupsAvailable(w) {
		return
	}
	group, ok := lookupGroup(w, r)
	if !ok {
		return
	}
	opts, ok := boolParams(w, r, "replace")
	if !ok {
		return
	}
	var entries map[string]string
	if !readStrictJSON(w, r, maxImportBytes, &entries) {
		return
	}
	if entries == nil {
		writeStatusError(w, http.StatusBadRequest, "body must be a JSON object of key: value (text)")
		return
	}
	for k, v := range entries {
		if err := checkLookupKey(k); err != nil {
			writeStatusError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := checkLookupValue(k, v); err != nil {
			writeStatusError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := s.cfg.Lookups.LookupPut(r.Context(), group, entries, opts["replace"]); err != nil {
		writeLookupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"imported": len(entries)})
}

// handleLookupBatch looks up many keys at once.
func (s *Server) handleLookupBatch(w http.ResponseWriter, r *http.Request) {
	if !s.lookupsAvailable(w) {
		return
	}
	group, ok := lookupGroup(w, r)
	if !ok {
		return
	}
	var body struct {
		Keys []string `json:"keys"`
	}
	if !readItemsBody(w, r, &body) {
		return
	}
	if len(body.Keys) == 0 || len(body.Keys) > maxLookupBatch {
		writeStatusError(w, http.StatusBadRequest, fmt.Sprintf("keys must list 1-%d keys", maxLookupBatch))
		return
	}
	found, err := s.cfg.Lookups.LookupGet(r.Context(), group, body.Keys)
	if err != nil {
		writeLookupError(w, err)
		return
	}
	missing := []string{}
	for _, k := range body.Keys {
		if _, ok := found[k]; !ok {
			missing = append(missing, k)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"found": found, "missing": missing})
}

func (s *Server) handleLookupGet(w http.ResponseWriter, r *http.Request) {
	if !s.lookupsAvailable(w) {
		return
	}
	group, key, ok := lookupKey(w, r)
	if !ok {
		return
	}
	found, err := s.cfg.Lookups.LookupGet(r.Context(), group, []string{key})
	if err != nil {
		writeLookupError(w, err)
		return
	}
	v, ok := found[key]
	if !ok {
		writeLookupError(w, ErrLookupNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": key, "value": v})
}

// handleLookupExists answers {"exists": true|false} (never 404 for a key).
func (s *Server) handleLookupExists(w http.ResponseWriter, r *http.Request) {
	if !s.lookupsAvailable(w) {
		return
	}
	group, key, ok := lookupKey(w, r)
	if !ok {
		return
	}
	found, err := s.cfg.Lookups.LookupGet(r.Context(), group, []string{key})
	if err != nil {
		writeLookupError(w, err)
		return
	}
	_, exists := found[key]
	writeJSON(w, http.StatusOK, map[string]bool{"exists": exists})
}

func (s *Server) handleLookupPut(w http.ResponseWriter, r *http.Request) {
	if !s.lookupsAvailable(w) {
		return
	}
	group, key, ok := lookupKey(w, r)
	if !ok {
		return
	}
	var body struct {
		Value *string `json:"value"`
	}
	if !readItemsBody(w, r, &body) {
		return
	}
	if body.Value == nil {
		writeStatusError(w, http.StatusBadRequest, `body must be {"value": "text"}`)
		return
	}
	if err := checkLookupValue(key, *body.Value); err != nil {
		writeStatusError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.cfg.Lookups.LookupPut(r.Context(), group, map[string]string{key: *body.Value}, false); err != nil {
		writeLookupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": key, "value": *body.Value})
}

func (s *Server) handleLookupDelete(w http.ResponseWriter, r *http.Request) {
	if !s.lookupsAvailable(w) {
		return
	}
	group, key, ok := lookupKey(w, r)
	if !ok {
		return
	}
	if err := s.cfg.Lookups.LookupDelete(r.Context(), group, key); err != nil {
		writeLookupError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
