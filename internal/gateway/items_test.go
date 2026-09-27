package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// memItems is an in-memory ItemStore; kind "broken" fails.
type memItems map[string]map[string]json.RawMessage

func (m memItems) ListItems(_ context.Context, kind string) (map[string]json.RawMessage, error) {
	if kind == "broken" {
		return nil, errors.New("disk")
	}
	return m[kind], nil
}

func (m memItems) GetItem(_ context.Context, kind, name string) (json.RawMessage, error) {
	v, ok := m[kind][name]
	if !ok {
		return nil, ErrItemNotFound
	}
	return v, nil
}

func (m memItems) PutItem(_ context.Context, kind, name string, v json.RawMessage) error {
	if m[kind] == nil {
		m[kind] = map[string]json.RawMessage{}
	}
	m[kind][name] = v
	return nil
}

func (m memItems) DeleteItem(_ context.Context, kind, name string) error {
	if _, ok := m[kind][name]; !ok {
		return ErrItemNotFound
	}
	delete(m[kind], name)
	return nil
}

func (m memItems) ReplaceItems(_ context.Context, kind string, items map[string]json.RawMessage) error {
	m[kind] = items
	return nil
}

func TestItemHandlers(t *testing.T) {
	cfg := Config{Items: memItems{}}
	tests := []struct {
		name, method, path, body string
		status                   int
		want                     string
	}{
		{"replace map", http.MethodPut, "/api/v1/configmap", `{"region":"eu","db.host":"x"}`, http.StatusOK, `"region":"eu"`},
		{"list map", http.MethodGet, "/api/v1/configmap", ``, http.StatusOK, `"db.host":"x"`},
		{"map value must be a string", http.MethodPut, "/api/v1/configmap", `{"n":5}`, http.StatusBadRequest, "must be a string"},
		{"bad name", http.MethodPut, "/api/v1/configmap", `{"a b":"x"}`, http.StatusBadRequest, "must be 1-128 characters"},
		{"not an object", http.MethodPut, "/api/v1/configmap", `null`, http.StatusBadRequest, "JSON object"},
		{"trailing data", http.MethodPut, "/api/v1/configmap", `{} {}`, http.StatusBadRequest, "trailing data"},
		{"get", http.MethodGet, "/api/v1/configmap/region", ``, http.StatusOK, `{"name":"region","value":"eu"}`},
		{"get unknown", http.MethodGet, "/api/v1/configmap/zz", ``, http.StatusNotFound, "config map entry not found"},
		{"put", http.MethodPut, "/api/v1/scripts/deploy", `{"value":"log('hi')"}`, http.StatusOK, `"value":"log('hi')"`},
		{"put without value", http.MethodPut, "/api/v1/scripts/deploy", `{}`, http.StatusBadRequest, `{\"value\": ...}`},
		{"script must be a string", http.MethodPut, "/api/v1/scripts/deploy", `{"value":[1]}`, http.StatusBadRequest, "must be a string"},
		{"setting any JSON", http.MethodPut, "/api/v1/settings/retention", `{"value":{"days":30}}`, http.StatusOK, `"days":30`},
		{"delete", http.MethodDelete, "/api/v1/scripts/deploy", ``, http.StatusNoContent, ""},
		{"delete unknown", http.MethodDelete, "/api/v1/scripts/deploy", ``, http.StatusNotFound, "script not found"},
		{"too large", http.MethodPut, "/api/v1/settings", `{"a":"` + strings.Repeat("x", maxItemsBody) + `"}`, http.StatusRequestEntityTooLarge, "larger than 10 MiB"},
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
	for _, path := range []string{"/api/v1/configmap", "/api/v1/configmap/x"} {
		for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
			if method == http.MethodDelete && !strings.HasSuffix(path, "/x") {
				continue
			}
			rec := httptest.NewRecorder()
			New(Config{}).Router().ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(`{"value":"v"}`)))
			if rec.Code != http.StatusServiceUnavailable {
				t.Errorf("%s %s without a store: %d", method, path, rec.Code)
			}
		}
	}
	// A backend failure is a 500 without detail.
	k := itemKind{kind: "broken", label: "thing"}
	rec := httptest.NewRecorder()
	New(cfg).handleItemsList(k)(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "disk") {
		t.Errorf("backend failure: %d %s", rec.Code, rec.Body.String())
	}
}
