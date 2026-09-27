package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
)

// Snippet is a reusable piece of code (spec §5 code snippets).
type Snippet struct {
	Name        string `json:"name"`
	Library     string `json:"library,omitempty"`
	Description string `json:"description,omitempty"`
	Code        string `json:"code,omitempty"`
}

// SnippetLibrary groups snippets.
type SnippetLibrary struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// SnippetStore keeps snippets and snippet libraries. Save writes every
// entry or none: with create it fails with ErrSnippetExists (ErrLibraryExists)
// when one already exists, otherwise it creates or replaces.
type SnippetStore interface {
	ListSnippets(ctx context.Context) ([]Snippet, error)
	GetSnippet(ctx context.Context, name string) (Snippet, error)     // ErrSnippetNotFound
	SaveSnippets(ctx context.Context, s []Snippet, create bool) error // ErrSnippetExists, ErrLibraryNotFound
	DeleteSnippet(ctx context.Context, name string) error             // ErrSnippetNotFound
	ListLibraries(ctx context.Context) ([]SnippetLibrary, error)
	GetLibrary(ctx context.Context, name string) (SnippetLibrary, error)      // ErrLibraryNotFound
	SaveLibraries(ctx context.Context, l []SnippetLibrary, create bool) error // ErrLibraryExists
	DeleteLibrary(ctx context.Context, name string) error                     // ErrLibraryNotFound, ErrLibraryInUse
}

// Snippet store errors.
var (
	ErrSnippetNotFound = errors.New("snippet not found")
	ErrSnippetExists   = errors.New("a snippet with that name already exists")
	ErrLibraryNotFound = errors.New("snippet library not found")
	ErrLibraryExists   = errors.New("a snippet library with that name already exists")
	ErrLibraryInUse    = errors.New("snippet library still has snippets")
)

func (s *Server) snippetsAvailable(w http.ResponseWriter) bool {
	if s.cfg.Snippets == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "snippets unavailable")
		return false
	}
	return true
}

func writeSnippetError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrSnippetNotFound):
		writeStatusError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrSnippetExists), errors.Is(err, ErrLibraryExists), errors.Is(err, ErrLibraryInUse):
		writeStatusError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrLibraryNotFound):
		writeStatusError(w, http.StatusNotFound, err.Error())
	default:
		writeBackendError(w, err)
	}
}

// checkSnippets validates snippets and rejects repeated names.
func checkSnippets(list []Snippet) error {
	seen := map[string]bool{}
	for _, sn := range list {
		if err := checkName(sn.Name); err != nil {
			return err
		}
		if sn.Library != "" {
			if err := checkName(sn.Library); err != nil {
				return fmt.Errorf("library: %w", err)
			}
		}
		if seen[sn.Name] {
			return fmt.Errorf("snippet %q appears more than once", sn.Name)
		}
		seen[sn.Name] = true
	}
	return nil
}

// checkLibraries validates libraries and rejects repeated names.
func checkLibraries(list []SnippetLibrary) error {
	seen := map[string]bool{}
	for _, l := range list {
		if err := checkName(l.Name); err != nil {
			return err
		}
		if seen[l.Name] {
			return fmt.Errorf("library %q appears more than once", l.Name)
		}
		seen[l.Name] = true
	}
	return nil
}

// pathName returns the {name} path value, answering 400 when it is invalid.
func pathName(w http.ResponseWriter, r *http.Request) (string, bool) {
	name := r.PathValue("name")
	if err := checkName(name); err != nil {
		writeStatusError(w, http.StatusBadRequest, err.Error())
		return "", false
	}
	return name, true
}

func (s *Server) handleSnippetsList(w http.ResponseWriter, r *http.Request) {
	if !s.snippetsAvailable(w) {
		return
	}
	summary := false
	if v := r.URL.Query().Get("summary"); v != "" {
		var err error
		if summary, err = strconv.ParseBool(v); err != nil {
			writeStatusError(w, http.StatusBadRequest, "summary must be true or false")
			return
		}
	}
	list, err := s.cfg.Snippets.ListSnippets(r.Context())
	if err != nil {
		writeSnippetError(w, err)
		return
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	if summary {
		for i := range list {
			list[i].Code = ""
		}
	}
	writeJSON(w, http.StatusOK, append([]Snippet{}, list...))
}

// handleSnippetsSave serves POST (create one), PUT on the collection (create
// or replace many), and PUT on one snippet.
func (s *Server) handleSnippetsSave(w http.ResponseWriter, r *http.Request) {
	if !s.snippetsAvailable(w) {
		return
	}
	var list []Snippet
	status, create, one := http.StatusOK, r.Method == http.MethodPost, r.Method == http.MethodPost
	if name := r.PathValue("name"); name != "" {
		var sn Snippet
		if !readItemsBody(w, r, &sn) {
			return
		}
		if sn.Name != "" && sn.Name != name {
			writeStatusError(w, http.StatusBadRequest, "the name in the body does not match the path")
			return
		}
		sn.Name, one = name, true
		list = []Snippet{sn}
	} else if create {
		var sn Snippet
		if !readItemsBody(w, r, &sn) {
			return
		}
		list, status = []Snippet{sn}, http.StatusCreated
	} else if !readItemsBody(w, r, &list) {
		return
	}
	if list == nil {
		writeStatusError(w, http.StatusBadRequest, "body must be a JSON array of snippets")
		return
	}
	if err := checkSnippets(list); err != nil {
		writeStatusError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.cfg.Snippets.SaveSnippets(r.Context(), list, create); err != nil {
		writeSnippetError(w, err)
		return
	}
	if one {
		writeJSON(w, status, list[0])
		return
	}
	writeJSON(w, status, list)
}

func (s *Server) handleSnippetGet(w http.ResponseWriter, r *http.Request) {
	if !s.snippetsAvailable(w) {
		return
	}
	name, ok := pathName(w, r)
	if !ok {
		return
	}
	sn, err := s.cfg.Snippets.GetSnippet(r.Context(), name)
	if err != nil {
		writeSnippetError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sn)
}

func (s *Server) handleSnippetDelete(w http.ResponseWriter, r *http.Request) {
	if !s.snippetsAvailable(w) {
		return
	}
	name, ok := pathName(w, r)
	if !ok {
		return
	}
	if err := s.cfg.Snippets.DeleteSnippet(r.Context(), name); err != nil {
		writeSnippetError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleLibrariesList(w http.ResponseWriter, r *http.Request) {
	if !s.snippetsAvailable(w) {
		return
	}
	list, err := s.cfg.Snippets.ListLibraries(r.Context())
	if err != nil {
		writeSnippetError(w, err)
		return
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	writeJSON(w, http.StatusOK, append([]SnippetLibrary{}, list...))
}

// handleLibrariesSave mirrors handleSnippetsSave for libraries.
func (s *Server) handleLibrariesSave(w http.ResponseWriter, r *http.Request) {
	if !s.snippetsAvailable(w) {
		return
	}
	var list []SnippetLibrary
	status, create, one := http.StatusOK, r.Method == http.MethodPost, r.Method == http.MethodPost
	if name := r.PathValue("name"); name != "" {
		var l SnippetLibrary
		if !readItemsBody(w, r, &l) {
			return
		}
		if l.Name != "" && l.Name != name {
			writeStatusError(w, http.StatusBadRequest, "the name in the body does not match the path")
			return
		}
		l.Name, one = name, true
		list = []SnippetLibrary{l}
	} else if create {
		var l SnippetLibrary
		if !readItemsBody(w, r, &l) {
			return
		}
		list, status = []SnippetLibrary{l}, http.StatusCreated
	} else if !readItemsBody(w, r, &list) {
		return
	}
	if list == nil {
		writeStatusError(w, http.StatusBadRequest, "body must be a JSON array of libraries")
		return
	}
	if err := checkLibraries(list); err != nil {
		writeStatusError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.cfg.Snippets.SaveLibraries(r.Context(), list, create); err != nil {
		writeSnippetError(w, err)
		return
	}
	if one {
		writeJSON(w, status, list[0])
		return
	}
	writeJSON(w, status, list)
}

func (s *Server) handleLibraryGet(w http.ResponseWriter, r *http.Request) {
	if !s.snippetsAvailable(w) {
		return
	}
	name, ok := pathName(w, r)
	if !ok {
		return
	}
	l, err := s.cfg.Snippets.GetLibrary(r.Context(), name)
	if err != nil {
		writeSnippetError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) handleLibraryDelete(w http.ResponseWriter, r *http.Request) {
	if !s.snippetsAvailable(w) {
		return
	}
	name, ok := pathName(w, r)
	if !ok {
		return
	}
	if err := s.cfg.Snippets.DeleteLibrary(r.Context(), name); err != nil {
		writeSnippetError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
