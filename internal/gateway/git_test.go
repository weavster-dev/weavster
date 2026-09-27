package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeGit records the last commit and serves fixed answers; err makes
// every call fail.
type fakeGit struct {
	committed *ConfigBundle
	author    *string
	err       error
}

func (f fakeGit) GitInfo(context.Context) (GitInfo, error) {
	return GitInfo{Path: "/repo", Branch: "main", Head: "abc"}, f.err
}

func (f fakeGit) GitCommit(_ context.Context, live ConfigBundle, _ string, author string) (GitCommitResult, error) {
	*f.committed, *f.author = live, author
	return GitCommitResult{Committed: true, Head: "def", Changed: []string{"flows/a.yaml"}}, f.err
}

func (f fakeGit) GitLog(_ context.Context, path string, limit int) ([]GitRevision, error) {
	return []GitRevision{{Hash: "abc", Message: path + " " + string(rune('0'+limit%10)), Author: "admin", At: time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)}}, f.err
}

func (f fakeGit) GitContent(_ context.Context, path, rev string) ([]byte, error) {
	if path == "missing.yaml" {
		return nil, ErrGitNotFound
	}
	return []byte(path + "@" + rev), f.err
}

func TestGitHandlers(t *testing.T) {
	var committed ConfigBundle
	var author string
	ports := func(err error) Config {
		return Config{
			Git: fakeGit{committed: &committed, author: &author, err: err}, Transfer: fakeTransfer{},
			Alerts: &memAlerts{alerts: map[string]Alert{}}, Items: memItems{},
			Snippets: &memSnippets{snippets: map[string]Snippet{}, libraries: map[string]SnippetLibrary{}},
		}
	}
	for _, tt := range []struct {
		name, method, path, body string
		cfg                      Config
		status                   int
		want                     string
	}{
		{"info", http.MethodGet, "/api/v1/git", "", ports(nil), http.StatusOK, `{"path":"/repo","branch":"main","head":"abc"}`},
		{"commit", http.MethodPost, "/api/v1/git/commit", `{"message":"nightly"}`, ports(nil), http.StatusOK, `"committed":true,"head":"def","changed":["flows/a.yaml"]`},
		{"commit without message", http.MethodPost, "/api/v1/git/commit", `{"message":"  "}`, ports(nil), http.StatusBadRequest, "message is required"},
		{"commit unknown field", http.MethodPost, "/api/v1/git/commit", `{"msg":"x"}`, ports(nil), http.StatusBadRequest, "invalid JSON body"},
		{"commit without config ports", http.MethodPost, "/api/v1/git/commit", `{"message":"x"}`, Config{Git: fakeGit{}}, http.StatusServiceUnavailable, "configuration export and import unavailable"},
		{"commit fails", http.MethodPost, "/api/v1/git/commit", `{"message":"x"}`, ports(errDisk), http.StatusInternalServerError, "internal error"},
		{"log", http.MethodGet, "/api/v1/git/log", "", ports(nil), http.StatusOK, `"message":" 0"`},
		{"file history", http.MethodGet, "/api/v1/git/log?path=flows/a.yaml&limit=7", "", ports(nil), http.StatusOK, `"message":"flows/a.yaml 7"`},
		{"bad limit", http.MethodGet, "/api/v1/git/log?limit=1001", "", ports(nil), http.StatusBadRequest, "limit must be between 1 and 1000"},
		{"log fails", http.MethodGet, "/api/v1/git/log", "", ports(errDisk), http.StatusInternalServerError, "internal error"},
		{"content at head", http.MethodGet, "/api/v1/git/content?path=flows/a.yaml", "", ports(nil), http.StatusOK, "flows/a.yaml@HEAD"},
		{"content at rev", http.MethodGet, "/api/v1/git/content?path=flows/a.yaml&rev=HEAD~1", "", ports(nil), http.StatusOK, "flows/a.yaml@HEAD~1"},
		{"content without path", http.MethodGet, "/api/v1/git/content", "", ports(nil), http.StatusBadRequest, "path is required"},
		{"content not found", http.MethodGet, "/api/v1/git/content?path=missing.yaml", "", ports(nil), http.StatusNotFound, "not found in the repository"},
		{"info fails", http.MethodGet, "/api/v1/git", "", ports(errDisk), http.StatusInternalServerError, "internal error"},
		{"not configured", http.MethodGet, "/api/v1/git", "", Config{}, http.StatusServiceUnavailable, "git.path"},
		{"log not configured", http.MethodGet, "/api/v1/git/log", "", Config{}, http.StatusServiceUnavailable, "git.path"},
		{"content not configured", http.MethodGet, "/api/v1/git/content?path=a", "", Config{}, http.StatusServiceUnavailable, "git.path"},
		{"commit not configured", http.MethodPost, "/api/v1/git/commit", `{"message":"x"}`, Config{}, http.StatusServiceUnavailable, "git.path"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(tt.cfg).Router().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("got %d %.300s; want %d containing %q", rec.Code, rec.Body.String(), tt.status, tt.want)
			}
		})
	}
	// The commit gets the live configuration without the config map, and
	// "weavster" as author when no one is signed in.
	if committed.Format != ConfigFormat || committed.ConfigMap != nil || author != "weavster" {
		t.Errorf("committed %+v by %q", committed, author)
	}
}
