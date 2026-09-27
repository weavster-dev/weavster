package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// memSnippets is an in-memory SnippetStore; fail makes every call fail.
type memSnippets struct {
	snippets  map[string]Snippet
	libraries map[string]SnippetLibrary
	fail      bool
}

var errDisk = errors.New("disk")

func (m *memSnippets) ListSnippets(context.Context) ([]Snippet, error) {
	if m.fail {
		return nil, errDisk
	}
	out := []Snippet{}
	for _, s := range m.snippets {
		out = append(out, s)
	}
	return out, nil
}

func (m *memSnippets) GetSnippet(_ context.Context, name string) (Snippet, error) {
	s, ok := m.snippets[name]
	if !ok {
		return s, ErrSnippetNotFound
	}
	return s, nil
}

func (m *memSnippets) SaveSnippets(_ context.Context, list []Snippet, create bool) error {
	if m.fail {
		return errDisk
	}
	for _, s := range list {
		if _, ok := m.snippets[s.Name]; ok && create {
			return ErrSnippetExists
		}
		if _, ok := m.libraries[s.Library]; s.Library != "" && !ok {
			return ErrLibraryNotFound
		}
	}
	for _, s := range list {
		m.snippets[s.Name] = s
	}
	return nil
}

func (m *memSnippets) DeleteSnippet(_ context.Context, name string) error {
	if _, ok := m.snippets[name]; !ok {
		return ErrSnippetNotFound
	}
	delete(m.snippets, name)
	return nil
}

func (m *memSnippets) ListLibraries(context.Context) ([]SnippetLibrary, error) {
	if m.fail {
		return nil, errDisk
	}
	out := []SnippetLibrary{}
	for _, l := range m.libraries {
		out = append(out, l)
	}
	return out, nil
}

func (m *memSnippets) GetLibrary(_ context.Context, name string) (SnippetLibrary, error) {
	l, ok := m.libraries[name]
	if !ok {
		return l, ErrLibraryNotFound
	}
	return l, nil
}

func (m *memSnippets) SaveLibraries(_ context.Context, list []SnippetLibrary, create bool) error {
	if m.fail {
		return errDisk
	}
	for _, l := range list {
		if _, ok := m.libraries[l.Name]; ok && create {
			return ErrLibraryExists
		}
	}
	for _, l := range list {
		m.libraries[l.Name] = l
	}
	return nil
}

func (m *memSnippets) DeleteLibrary(_ context.Context, name string) error {
	if _, ok := m.libraries[name]; !ok {
		return ErrLibraryNotFound
	}
	for _, s := range m.snippets {
		if s.Library == name {
			return ErrLibraryInUse
		}
	}
	delete(m.libraries, name)
	return nil
}

func TestSnippetHandlers(t *testing.T) {
	store := &memSnippets{snippets: map[string]Snippet{}, libraries: map[string]SnippetLibrary{}}
	cfg := Config{Snippets: store}
	tests := []struct {
		name, method, path, body string
		status                   int
		want                     string
	}{
		{"create library", http.MethodPost, "/api/v1/snippet-libraries", `{"name":"hl7","description":"HL7 helpers"}`, http.StatusCreated, `"name":"hl7"`},
		{"library exists", http.MethodPost, "/api/v1/snippet-libraries", `{"name":"hl7"}`, http.StatusConflict, "already exists"},
		{"bulk libraries", http.MethodPut, "/api/v1/snippet-libraries", `[{"name":"x12"},{"name":"util"}]`, http.StatusOK, `"name":"util"`},
		{"repeated library", http.MethodPut, "/api/v1/snippet-libraries", `[{"name":"a"},{"name":"a"}]`, http.StatusBadRequest, "more than once"},
		{"libraries null", http.MethodPut, "/api/v1/snippet-libraries", `null`, http.StatusBadRequest, "JSON array"},
		{"put library", http.MethodPut, "/api/v1/snippet-libraries/util", `{"description":"misc"}`, http.StatusOK, `"description":"misc"`},
		{"library name mismatch", http.MethodPut, "/api/v1/snippet-libraries/util", `{"name":"other"}`, http.StatusBadRequest, "does not match"},
		{"list libraries", http.MethodGet, "/api/v1/snippet-libraries", ``, http.StatusOK, `[{"name":"hl7","description":"HL7 helpers"},{"name":"util","description":"misc"},{"name":"x12"}]`},
		{"get library", http.MethodGet, "/api/v1/snippet-libraries/x12", ``, http.StatusOK, `{"name":"x12"}`},
		{"create snippet", http.MethodPost, "/api/v1/snippets", `{"name":"pid","library":"hl7","code":"get('PID')"}`, http.StatusCreated, `"code":"get('PID')"`},
		{"snippet exists", http.MethodPost, "/api/v1/snippets", `{"name":"pid"}`, http.StatusConflict, "already exists"},
		{"unknown library", http.MethodPost, "/api/v1/snippets", `{"name":"x","library":"nope"}`, http.StatusNotFound, "snippet library not found"},
		{"bad library name", http.MethodPost, "/api/v1/snippets", `{"name":"x","library":"a b"}`, http.StatusBadRequest, "library: name"},
		{"bad snippet name", http.MethodPost, "/api/v1/snippets", `{"code":"x"}`, http.StatusBadRequest, "must be 1-128"},
		{"unknown field", http.MethodPost, "/api/v1/snippets", `{"name":"x","cod":"x"}`, http.StatusBadRequest, "unknown field"},
		{"bulk snippets", http.MethodPut, "/api/v1/snippets", `[{"name":"trim","code":"trim()"},{"name":"pid","library":"hl7","code":"v2"}]`, http.StatusOK, `"name":"trim"`},
		{"repeated snippet", http.MethodPut, "/api/v1/snippets", `[{"name":"a"},{"name":"a"}]`, http.StatusBadRequest, "more than once"},
		{"snippets null", http.MethodPut, "/api/v1/snippets", `null`, http.StatusBadRequest, "JSON array"},
		{"put snippet", http.MethodPut, "/api/v1/snippets/trim", `{"code":"trim2()"}`, http.StatusOK, `{"name":"trim","code":"trim2()"}`},
		{"null snippet", http.MethodPut, "/api/v1/snippets/trim", `null`, http.StatusBadRequest, "JSON snippet object"},
		{"null library", http.MethodPost, "/api/v1/snippet-libraries", `null`, http.StatusBadRequest, "JSON library object"},
		{"snippet name mismatch", http.MethodPut, "/api/v1/snippets/trim", `{"name":"x"}`, http.StatusBadRequest, "does not match"},
		{"get snippet", http.MethodGet, "/api/v1/snippets/pid", ``, http.StatusOK, `"code":"v2"`},
		{"get bad name", http.MethodGet, "/api/v1/snippets/a%20b", ``, http.StatusBadRequest, "must be 1-128"},
		{"get missing", http.MethodGet, "/api/v1/snippets/zz", ``, http.StatusNotFound, "snippet not found"},
		{"list", http.MethodGet, "/api/v1/snippets", ``, http.StatusOK, `[{"name":"pid","library":"hl7","code":"v2"},{"name":"trim","code":"trim2()"}]`},
		{"summary", http.MethodGet, "/api/v1/snippets?summary=true", ``, http.StatusOK, `[{"name":"pid","library":"hl7"},{"name":"trim"}]`},
		{"bad summary", http.MethodGet, "/api/v1/snippets?summary=maybe", ``, http.StatusBadRequest, "summary must be"},
		{"library in use", http.MethodDelete, "/api/v1/snippet-libraries/hl7", ``, http.StatusConflict, "still has snippets"},
		{"delete snippet", http.MethodDelete, "/api/v1/snippets/pid", ``, http.StatusNoContent, ""},
		{"delete missing snippet", http.MethodDelete, "/api/v1/snippets/pid", ``, http.StatusNotFound, "snippet not found"},
		{"delete library", http.MethodDelete, "/api/v1/snippet-libraries/hl7", ``, http.StatusNoContent, ""},
		{"delete missing library", http.MethodDelete, "/api/v1/snippet-libraries/hl7", ``, http.StatusNotFound, "snippet library not found"},
		{"delete bad library name", http.MethodDelete, "/api/v1/snippet-libraries/a%20b", ``, http.StatusBadRequest, "must be 1-128"},
		{"get missing library", http.MethodGet, "/api/v1/snippet-libraries/hl7", ``, http.StatusNotFound, "snippet library not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(cfg).Router().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("got %d %q; want %d containing %q", rec.Code, rec.Body.String(), tt.status, tt.want)
			}
		})
	}
}

func TestSnippetHandlersUnavailableAndFailing(t *testing.T) {
	failing := &memSnippets{fail: true}
	for _, tt := range []struct {
		cfg          Config
		method, path string
		status       int
	}{
		{Config{}, http.MethodGet, "/api/v1/snippets", http.StatusServiceUnavailable},
		{Config{}, http.MethodPost, "/api/v1/snippets", http.StatusServiceUnavailable},
		{Config{}, http.MethodGet, "/api/v1/snippets/a", http.StatusServiceUnavailable},
		{Config{}, http.MethodDelete, "/api/v1/snippets/a", http.StatusServiceUnavailable},
		{Config{}, http.MethodGet, "/api/v1/snippet-libraries", http.StatusServiceUnavailable},
		{Config{}, http.MethodPut, "/api/v1/snippet-libraries", http.StatusServiceUnavailable},
		{Config{}, http.MethodGet, "/api/v1/snippet-libraries/a", http.StatusServiceUnavailable},
		{Config{}, http.MethodDelete, "/api/v1/snippet-libraries/a", http.StatusServiceUnavailable},
		{Config{Snippets: failing}, http.MethodGet, "/api/v1/snippets", http.StatusInternalServerError},
		{Config{Snippets: failing}, http.MethodGet, "/api/v1/snippet-libraries", http.StatusInternalServerError},
		{Config{Snippets: failing}, http.MethodPut, "/api/v1/snippets", http.StatusInternalServerError},
		{Config{Snippets: failing}, http.MethodPut, "/api/v1/snippet-libraries", http.StatusInternalServerError},
	} {
		body := `[{"name":"a"}]`
		if tt.method == http.MethodPost {
			body = `{"name":"a"}`
		}
		rec := httptest.NewRecorder()
		New(tt.cfg).Router().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, strings.NewReader(body)))
		if rec.Code != tt.status {
			t.Errorf("%s %s: %d %s; want %d", tt.method, tt.path, rec.Code, rec.Body.String(), tt.status)
		}
	}
}
