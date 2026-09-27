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

// nameRef lets saveNamed set and read a document's name.
func (sn *Snippet) nameRef() *string       { return &sn.Name }
func (l *SnippetLibrary) nameRef() *string { return &l.Name }

// namedDoc is a pointer to a document with a name.
type namedDoc[T any] interface {
	*T
	nameRef() *string
}

// checkNames validates each name and rejects repeated names.
func checkNames[T any, P namedDoc[T]](list []T, noun string) error {
	seen := map[string]bool{}
	for i := range list {
		name := *P(&list[i]).nameRef()
		if err := checkName(name); err != nil {
			return err
		}
		if seen[name] {
			return fmt.Errorf("%s %q appears more than once", noun, name)
		}
		seen[name] = true
	}
	return nil
}

// saveNamed reads and saves documents: with many, a JSON array (create or
// replace); otherwise one document, which replaces the {name} in the path
// or, without one, is created (201). check adds per-document validation;
// save writes the list; writeErr reports a save error.
func saveNamed[T any, P namedDoc[T]](w http.ResponseWriter, r *http.Request, noun string, many bool, check func(T) error,
	save func(context.Context, []T, bool) error, writeErr func(http.ResponseWriter, error)) {
	var list []T
	name := r.PathValue("name")
	status, create, one := http.StatusOK, !many && name == "", !many
	if !many {
		var ptr *T
		if !readItemsBody(w, r, &ptr) {
			return
		}
		if ptr == nil {
			writeStatusError(w, http.StatusBadRequest, "body must be a JSON "+noun+" object")
			return
		}
		doc := *ptr
		if ref := P(&doc).nameRef(); name != "" {
			if *ref != "" && *ref != name {
				writeStatusError(w, http.StatusBadRequest, "the name or id in the body does not match the path")
				return
			}
			*ref = name
		} else {
			status = http.StatusCreated
		}
		list = []T{doc}
	} else if !readItemsBody(w, r, &list) {
		return
	}
	if list == nil {
		writeStatusError(w, http.StatusBadRequest, "body must be a JSON array of "+noun+" objects")
		return
	}
	err := checkNames[T, P](list, noun)
	for i := 0; err == nil && i < len(list); i++ {
		err = check(list[i])
	}
	if err != nil {
		writeStatusError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := save(r.Context(), list, create); err != nil {
		writeErr(w, err)
		return
	}
	if one {
		writeJSON(w, status, list[0])
		return
	}
	writeJSON(w, status, list)
}

// isBulk reports a PUT on a collection, which takes a JSON array.
func isBulk(r *http.Request) bool {
	return r.Method == http.MethodPut && r.PathValue("name") == ""
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

func (s *Server) handleSnippetsSave(w http.ResponseWriter, r *http.Request) {
	if !s.snippetsAvailable(w) {
		return
	}
	saveNamed(w, r, "snippet", isBulk(r), func(sn Snippet) error {
		if sn.Library == "" {
			return nil
		}
		if err := checkName(sn.Library); err != nil {
			return fmt.Errorf("library: %w", err)
		}
		return nil
	}, s.cfg.Snippets.SaveSnippets, writeSnippetError)
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

func (s *Server) handleLibrariesSave(w http.ResponseWriter, r *http.Request) {
	if !s.snippetsAvailable(w) {
		return
	}
	saveNamed(w, r, "library", isBulk(r), func(SnippetLibrary) error { return nil }, s.cfg.Snippets.SaveLibraries, writeSnippetError)
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
