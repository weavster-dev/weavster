package main

import (
	"net/http"
	"strings"
	"testing"
)

// TestLookups: dynamic lookups are imported, matched by prefix, read one
// by one and in batches, changed, and deleted over the API; reads and
// writes need lookups:view and lookups:edit; entries survive a restart with
// the SQLite store.
func TestLookups(t *testing.T) {
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n")
	args := []string{"server", "--config", cfg}
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	stop := startCLI(t, args, c.base+"/api/openapi.yaml")

	for _, s := range []struct {
		name, method, path, body string
		status                   int
		want                     string
	}{
		{"import", http.MethodPost, "/api/v1/lookups/facility/import", `{"LAB-01":"Central Lab","LAB-02":"North Lab","RAD 01":"Radiology"}`, http.StatusOK, `{"imported":3}`},
		{"groups", http.MethodGet, "/api/v1/lookups", ``, http.StatusOK, `[{"name":"facility","entries":3}]`},
		{"matching", http.MethodGet, "/api/v1/lookups/facility?prefix=LAB-", ``, http.StatusOK, `{"LAB-01":"Central Lab","LAB-02":"North Lab"}`},
		{"get with a space", http.MethodGet, "/api/v1/lookups/facility/RAD%2001", ``, http.StatusOK, `"value":"Radiology"`},
		{"batch", http.MethodPost, "/api/v1/lookups/facility/batch", `{"keys":["LAB-01","XX"]}`, http.StatusOK, `"missing":["XX"]`},
		{"exists", http.MethodGet, "/api/v1/lookups/facility/LAB-02/exists", ``, http.StatusOK, `{"exists":true}`},
		{"put", http.MethodPut, "/api/v1/lookups/facility/LAB-02", `{"value":"North Campus Lab"}`, http.StatusOK, `"North Campus Lab"`},
		{"delete", http.MethodDelete, "/api/v1/lookups/facility/RAD%2001", ``, http.StatusNoContent, ""},
		{"unknown", http.MethodGet, "/api/v1/lookups/facility/RAD%2001", ``, http.StatusNotFound, "lookup not found"},
		{"not text", http.MethodPost, "/api/v1/lookups/facility/import", `{"a":1}`, http.StatusBadRequest, "invalid JSON body"},
	} {
		if code, body, _ := c.do(s.method, s.path, s.body, admin); code != s.status || !strings.Contains(body, s.want) {
			t.Errorf("%s: %d %q; want %d containing %q", s.name, code, body, s.status, s.want)
		}
	}
	c.do(http.MethodPost, "/api/v1/users", `{"username":"reader","password":"Reader-Passw0rd","permissions":["lookups:view"],"mustChangePassword":false}`, admin)
	reader := basic("reader", "Reader-Passw0rd")
	if code, _, _ := c.do(http.MethodGet, "/api/v1/lookups/facility/LAB-01", "", reader); code != http.StatusOK {
		t.Errorf("reader get: %d", code)
	}
	if code, body, _ := c.do(http.MethodPut, "/api/v1/lookups/facility/LAB-01", `{"value":"x"}`, reader); code != http.StatusForbidden || !strings.Contains(body, "lookups:edit") {
		t.Errorf("reader put: %d %s", code, body)
	}
	stop()

	stop = startCLI(t, args, c.base+"/api/openapi.yaml")
	defer stop()
	if _, body, _ := c.do(http.MethodGet, "/api/v1/lookups/facility", "", admin); body != `{"LAB-01":"Central Lab","LAB-02":"North Campus Lab"}`+"\n" {
		t.Errorf("after restart = %s", body)
	}
	if code, _, _ := c.do(http.MethodPost, "/api/v1/lookups/facility/import?replace=true", `{"LAB-09":"New"}`, admin); code != http.StatusOK {
		t.Errorf("replace: %d", code)
	}
	if code, body, _ := c.do(http.MethodDelete, "/api/v1/lookups/facility", "", admin); code != http.StatusNoContent {
		t.Errorf("delete group: %d %s", code, body)
	}
	if _, body, _ := c.do(http.MethodGet, "/api/v1/lookups", "", admin); body != "[]\n" {
		t.Errorf("groups after delete = %s", body)
	}
}
