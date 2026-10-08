package main

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestAllFlowsLifecycleAndStats covers the all-flows actions (dependency
// order, skipped flows) and the statistics operations: all flows, reset of
// current counters, and reset of lifetime totals.
func TestAllFlowsLifecycleAndStats(t *testing.T) {
	c := startComposed(t, serverconfig.Default(), io.Discard)
	admin := basic(bootstrapAdmin, testAdminPassword)
	dir := t.TempDir()
	for _, body := range []string{
		`{"id":"a","enabled":true,"destinations":[{"name":"out","type":"file","dir":"` + dir + `"}]}`,
		`{"id":"b","enabled":true,"dependsOn":["a"]}`,
		`{"id":"c"}`,
	} {
		if code, resp, _ := c.do(http.MethodPost, "/api/v1/flows", body, admin); code != http.StatusCreated {
			t.Fatalf("create: %d %q", code, resp)
		}
	}
	steps := []struct {
		method, path, body string
		status             int
		contains           []string
	}{
		{http.MethodPost, "/api/v1/flows/deploy-all", ``, http.StatusOK, []string{`"changed":["a","b"]`, `{"id":"c","reason":"disabled"}`}},
		{http.MethodPost, "/api/v1/flows/start-all", ``, http.StatusOK, []string{`"changed":["a","b"]`, `"id":"c","reason":"invalid lifecycle transition`}},
		{http.MethodPost, "/api/v1/flows/pause-all", ``, http.StatusOK, []string{`"changed":["b","a"]`}}, // dependents first
		{http.MethodPost, "/api/v1/flows/resume-all", ``, http.StatusOK, []string{`"changed":["a","b"]`}},
		{http.MethodPost, "/api/v1/flows/a/messages", `x`, http.StatusAccepted, []string{`"status":"sent"`}},
		{http.MethodGet, "/api/v1/flows/stats", ``, http.StatusOK, []string{`"a":{"received":1,`, `"c":{"received":0,`}},
		{http.MethodPost, "/api/v1/flows/a/stats/reset", ``, http.StatusNoContent, nil},
		{http.MethodGet, "/api/v1/flows/a/stats", ``, http.StatusOK, []string{`"received":0,`}},
		{http.MethodGet, "/api/v1/flows/a/stats?lifetime=true", ``, http.StatusOK, []string{`"received":1,`}},
		{http.MethodPost, "/api/v1/flows/stats/reset?lifetime=true", ``, http.StatusNoContent, nil},
		{http.MethodGet, "/api/v1/flows/stats?lifetime=true", ``, http.StatusOK, []string{`"a":{"received":0,`}},
		{http.MethodPost, "/api/v1/flows/zz/stats/reset", ``, http.StatusNotFound, []string{`"code":"NOT_FOUND"`}},
		{http.MethodPost, "/api/v1/flows/stats/reset?lifetime=maybe", ``, http.StatusBadRequest, []string{"lifetime must be true or false"}},
		{http.MethodPost, "/api/v1/flows/stop-all", ``, http.StatusOK, []string{`"changed":["b","a"]`}},
		{http.MethodPost, "/api/v1/flows/undeploy-all", ``, http.StatusOK, []string{`"changed":["b","a"]`}},
		// start-all is a reserved id, so GET finds no such flow.
		{http.MethodGet, "/api/v1/flows/start-all", ``, http.StatusNotFound, []string{"flow not found"}},
		{http.MethodPost, "/api/v1/flows", `{"id":"stats"}`, http.StatusBadRequest, []string{"is reserved"}},
	}
	for _, s := range steps {
		status, body, _ := c.do(s.method, s.path, s.body, admin)
		if status != s.status {
			t.Errorf("%s %s: %d %q, want %d", s.method, s.path, status, body, s.status)
			continue
		}
		for _, want := range s.contains {
			if !strings.Contains(body, want) {
				t.Errorf("%s %s: %q does not contain %q", s.method, s.path, body, want)
			}
		}
	}
}
