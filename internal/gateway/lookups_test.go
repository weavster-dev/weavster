package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

// memLookups is an in-memory LookupStore; fail makes every call fail.
type memLookups struct {
	groups map[string]map[string]string
	fail   bool
}

func (m *memLookups) LookupGroups(context.Context) (map[string]int, error) {
	if m.fail {
		return nil, errDisk
	}
	out := map[string]int{}
	for g, e := range m.groups {
		out[g] = len(e)
	}
	return out, nil
}

func (m *memLookups) LookupEntries(_ context.Context, g, prefix string, limit int) (map[string]string, error) {
	if m.fail {
		return nil, errDisk
	}
	var keys []string
	for k := range m.groups[g] {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if len(keys) > limit {
		keys = keys[:limit]
	}
	out := map[string]string{}
	for _, k := range keys {
		out[k] = m.groups[g][k]
	}
	return out, nil
}

func (m *memLookups) LookupGet(_ context.Context, g string, keys []string) (map[string]string, error) {
	if m.fail {
		return nil, errDisk
	}
	out := map[string]string{}
	for _, k := range keys {
		if v, ok := m.groups[g][k]; ok {
			out[k] = v
		}
	}
	return out, nil
}

func (m *memLookups) LookupPut(_ context.Context, g string, entries map[string]string, replace bool) error {
	if m.fail {
		return errDisk
	}
	if replace || m.groups[g] == nil {
		m.groups[g] = map[string]string{}
	}
	for k, v := range entries {
		m.groups[g][k] = v
	}
	return nil
}

func (m *memLookups) LookupDelete(_ context.Context, g, k string) error {
	if m.fail {
		return errDisk
	}
	if _, ok := m.groups[g][k]; !ok {
		return ErrLookupNotFound
	}
	delete(m.groups[g], k)
	return nil
}

func (m *memLookups) LookupDeleteGroup(_ context.Context, g string) error {
	if m.fail {
		return errDisk
	}
	if _, ok := m.groups[g]; !ok {
		return ErrLookupNotFound
	}
	delete(m.groups, g)
	return nil
}

func TestLookupHandlers(t *testing.T) {
	cfg := Config{Lookups: &memLookups{groups: map[string]map[string]string{}}}
	failing := Config{Lookups: &memLookups{fail: true}}
	tests := []struct {
		name, method, path, body string
		cfg                      Config
		status                   int
		want                     string
	}{
		{"import", http.MethodPost, "/api/v1/lookups/mrn/import", `{"A-1":"x","A-2":"y","B 1":"z"}`, cfg, http.StatusOK, `{"imported":3}`},
		{"import bad value", http.MethodPost, "/api/v1/lookups/mrn/import", `{"a":1}`, cfg, http.StatusBadRequest, "invalid JSON body"},
		{"import null", http.MethodPost, "/api/v1/lookups/mrn/import", `null`, cfg, http.StatusBadRequest, "JSON object"},
		{"import control key", http.MethodPost, "/api/v1/lookups/mrn/import", `{"a\u0001":"x"}`, cfg, http.StatusBadRequest, "control characters"},
		{"import big value", http.MethodPost, "/api/v1/lookups/mrn/import", `{"a":"` + strings.Repeat("x", maxLookupValue+1) + `"}`, cfg, http.StatusBadRequest, "64 KiB"},
		{"import bad flag", http.MethodPost, "/api/v1/lookups/mrn/import?replace=x", `{}`, cfg, http.StatusBadRequest, "replace must be"},
		{"bad group", http.MethodPost, "/api/v1/lookups/a%20b/import", `{}`, cfg, http.StatusBadRequest, "group: name"},
		{"groups", http.MethodGet, "/api/v1/lookups", ``, cfg, http.StatusOK, `[{"name":"mrn","entries":3}]`},
		{"matching", http.MethodGet, "/api/v1/lookups/mrn?prefix=A-&limit=1", ``, cfg, http.StatusOK, `{"A-1":"x"}`},
		{"matching bad limit", http.MethodGet, "/api/v1/lookups/mrn?limit=0", ``, cfg, http.StatusBadRequest, "limit must be"},
		{"get", http.MethodGet, "/api/v1/lookups/mrn/B%201", ``, cfg, http.StatusOK, `{"key":"B 1","value":"z"}`},
		{"get missing", http.MethodGet, "/api/v1/lookups/mrn/zz", ``, cfg, http.StatusNotFound, "lookup not found"},
		{"exists", http.MethodGet, "/api/v1/lookups/mrn/A-1/exists", ``, cfg, http.StatusOK, `{"exists":true}`},
		{"not exists", http.MethodGet, "/api/v1/lookups/mrn/zz/exists", ``, cfg, http.StatusOK, `{"exists":false}`},
		{"batch", http.MethodPost, "/api/v1/lookups/mrn/batch", `{"keys":["A-1","zz"]}`, cfg, http.StatusOK, `{"found":{"A-1":"x"},"missing":["zz"]}`},
		{"batch empty", http.MethodPost, "/api/v1/lookups/mrn/batch", `{"keys":[]}`, cfg, http.StatusBadRequest, "1-1000 keys"},
		{"batch repeats", http.MethodPost, "/api/v1/lookups/mrn/batch", `{"keys":["zz","A-1","zz"]}`, cfg, http.StatusOK, `"missing":["zz"]}`},
		{"batch bad key", http.MethodPost, "/api/v1/lookups/mrn/batch", `{"keys":["zz",""]}`, cfg, http.StatusBadRequest, "1-512 characters"},
		{"slash key", http.MethodPut, "/api/v1/lookups/mrn/A%2FB", `{"value":"slash"}`, cfg, http.StatusOK, `"key":"A/B"`},
		{"get slash key", http.MethodGet, "/api/v1/lookups/mrn/A%2FB", ``, cfg, http.StatusOK, `"value":"slash"`},
		{"comma and percent key", http.MethodPut, "/api/v1/lookups/mrn/a%2Cb%25c", `{"value":"x"}`, cfg, http.StatusOK, `"key":"a,b%c"`},
		{"delete slash key", http.MethodDelete, "/api/v1/lookups/mrn/A%2FB", ``, cfg, http.StatusNoContent, ""},
		{"put", http.MethodPut, "/api/v1/lookups/mrn/A-3", `{"value":"w"}`, cfg, http.StatusOK, `"value":"w"`},
		{"put no value", http.MethodPut, "/api/v1/lookups/mrn/A-3", `{}`, cfg, http.StatusBadRequest, `{\"value\": \"text\"}`},
		{"put long key", http.MethodPut, "/api/v1/lookups/mrn/" + strings.Repeat("k", maxLookupKey+1), `{"value":"w"}`, cfg, http.StatusBadRequest, "1-512 characters"},
		{"put big value", http.MethodPut, "/api/v1/lookups/mrn/k", `{"value":"` + strings.Repeat("x", maxLookupValue+1) + `"}`, cfg, http.StatusBadRequest, "64 KiB"},
		{"put NUL value", http.MethodPut, "/api/v1/lookups/mrn/k", `{"value":"a\u0000b"}`, cfg, http.StatusBadRequest, "without NUL"},
		{"delete", http.MethodDelete, "/api/v1/lookups/mrn/A-3", ``, cfg, http.StatusNoContent, ""},
		{"delete missing", http.MethodDelete, "/api/v1/lookups/mrn/A-3", ``, cfg, http.StatusNotFound, "lookup not found"},
		{"replace", http.MethodPost, "/api/v1/lookups/mrn/import?replace=true", `{"only":"1"}`, cfg, http.StatusOK, `{"imported":1}`},
		{"after replace", http.MethodGet, "/api/v1/lookups/mrn", ``, cfg, http.StatusOK, `{"only":"1"}`},
		{"delete group", http.MethodDelete, "/api/v1/lookups/mrn", ``, cfg, http.StatusNoContent, ""},
		{"delete missing group", http.MethodDelete, "/api/v1/lookups/mrn", ``, cfg, http.StatusNotFound, "lookup not found"},
		{"groups fail", http.MethodGet, "/api/v1/lookups", ``, failing, http.StatusInternalServerError, "internal error"},
		{"matching fail", http.MethodGet, "/api/v1/lookups/g", ``, failing, http.StatusInternalServerError, "internal error"},
		{"get fail", http.MethodGet, "/api/v1/lookups/g/k", ``, failing, http.StatusInternalServerError, "internal error"},
		{"exists fail", http.MethodGet, "/api/v1/lookups/g/k/exists", ``, failing, http.StatusInternalServerError, "internal error"},
		{"batch fail", http.MethodPost, "/api/v1/lookups/g/batch", `{"keys":["k"]}`, failing, http.StatusInternalServerError, "internal error"},
		{"put fail", http.MethodPut, "/api/v1/lookups/g/k", `{"value":"v"}`, failing, http.StatusInternalServerError, "internal error"},
		{"import fail", http.MethodPost, "/api/v1/lookups/g/import", `{"k":"v"}`, failing, http.StatusInternalServerError, "internal error"},
		{"delete fail", http.MethodDelete, "/api/v1/lookups/g/k", ``, failing, http.StatusInternalServerError, "internal error"},
		{"delete group fail", http.MethodDelete, "/api/v1/lookups/g", ``, failing, http.StatusInternalServerError, "internal error"},
		{"unavailable", http.MethodGet, "/api/v1/lookups", ``, Config{}, http.StatusServiceUnavailable, "lookups unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(tt.cfg).Router().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("got %d %.200q; want %d containing %q", rec.Code, rec.Body.String(), tt.status, tt.want)
			}
		})
	}
	for _, req := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/lookups/g"},
		{http.MethodGet, "/api/v1/lookups/g/k"},
		{http.MethodGet, "/api/v1/lookups/g/k/exists"},
		{http.MethodPost, "/api/v1/lookups/g/batch"},
		{http.MethodPost, "/api/v1/lookups/g/import"},
		{http.MethodPut, "/api/v1/lookups/g/k"},
		{http.MethodDelete, "/api/v1/lookups/g/k"},
		{http.MethodDelete, "/api/v1/lookups/g"},
	} {
		rec := httptest.NewRecorder()
		New(Config{}).Router().ServeHTTP(rec, httptest.NewRequest(req.method, req.path, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s unavailable = %d", req.method, req.path, rec.Code)
		}
	}
}
