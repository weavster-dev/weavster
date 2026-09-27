package gateway

import (
	"context"
	"fmt"
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
	return GitInfo{Branch: "main", Head: "abc"}, f.err
}

func (f fakeGit) GitCommit(_ context.Context, live ConfigBundle, _ string, author string) (GitCommitResult, error) {
	*f.committed, *f.author = live, author
	return GitCommitResult{Committed: true, Head: "def", Changed: []string{"flows/a.yaml"}}, f.err
}

func (f fakeGit) GitLog(_ context.Context, path string, limit int) ([]GitRevision, error) {
	return []GitRevision{{Hash: "abc", Message: path + " " + string(rune('0'+limit%10)), Author: "admin", At: time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)}}, f.err
}

func (f fakeGit) GitContent(_ context.Context, path, rev string) ([]byte, error) {
	switch path {
	case "missing.yaml":
		return nil, ErrGitNotFound
	case "README.md":
		return []byte("ops notes"), nil
	}
	return []byte(path + "@" + rev), f.err
}

// GitDocument maps revisions to fakePlanner documents: HEAD plans with
// changes, "same" without, "bad" is rejected by the planner.
func (f fakeGit) GitDocument(_ context.Context, rev string) ([]byte, string, error) {
	switch rev {
	case "HEAD":
		return []byte("changed"), "c0ffee", f.err
	case "same":
		return []byte("ok"), "5a3e", f.err
	case "invalid":
		return nil, "", fmt.Errorf("%w: flows/a.yaml: bad", ErrInvalidConfig)
	case "missing":
		return nil, "", ErrGitNotFound
	}
	return []byte(rev), "0ther", f.err
}

func (f fakeGit) GitRemote(context.Context) (GitRemoteStatus, error) {
	return GitRemoteStatus{URL: "https://example.com/r.git", Branch: "main", Head: "abc", RemoteHead: "abc"}, f.err
}

func (f fakeGit) GitPush(context.Context) (GitRemoteStatus, error) {
	if f.err != nil {
		return GitRemoteStatus{}, f.err
	}
	return GitRemoteStatus{Branch: "main", Head: "def", RemoteHead: "def"}, nil
}

func (f fakeGit) GitPull(context.Context) (GitPullResult, error) {
	return GitPullResult{Head: "abc", Dropped: []string{"def"}}, f.err
}

func (f fakeGit) GitDiff(_ context.Context, from, to string) (GitDiff, error) {
	if from == "missing" {
		return GitDiff{}, ErrGitNotFound
	}
	return GitDiff{From: from, To: to, Files: []GitFileChange{{Path: "flows/a.yaml", Status: "modified"}}}, f.err
}

func (f fakeGit) GitRestore(_ context.Context, rev, path, _, author string) (GitCommitResult, error) {
	if rev == "dirty" {
		return GitCommitResult{}, fmt.Errorf("%w: uncommitted changes", ErrGitConflict)
	}
	return GitCommitResult{Committed: true, Head: rev + ":" + path + ":" + author, Changed: []string{"flows/a.yaml"}}, f.err
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
		{"info", http.MethodGet, "/api/v1/git", "", ports(nil), http.StatusOK, `{"branch":"main","head":"abc"}`},
		{"commit", http.MethodPost, "/api/v1/git/commit", `{"message":"nightly"}`, ports(nil), http.StatusOK, `"committed":true,"head":"def","changed":["flows/a.yaml"]`},
		{"commit without message", http.MethodPost, "/api/v1/git/commit", `{"message":"  "}`, ports(nil), http.StatusBadRequest, "message is required"},
		{"commit unknown field", http.MethodPost, "/api/v1/git/commit", `{"msg":"x"}`, ports(nil), http.StatusBadRequest, "invalid JSON body"},
		{"commit without config ports", http.MethodPost, "/api/v1/git/commit", `{"message":"x"}`, Config{Git: fakeGit{}}, http.StatusServiceUnavailable, "configuration export and import unavailable"},
		{"commit fails", http.MethodPost, "/api/v1/git/commit", `{"message":"x"}`, ports(errDisk), http.StatusInternalServerError, "internal error"},
		{"commit name clash", http.MethodPost, "/api/v1/git/commit", `{"message":"x"}`, ports(fmt.Errorf("%w: flow/A and flow/a", ErrGitNameClash)), http.StatusConflict, "differ only in case"},
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
	rec := httptest.NewRecorder()
	New(ports(nil)).Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/git/content?path=README.md", nil))
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("README content type = %q", ct)
	}
	// The commit gets the live configuration without the config map, and
	// "weavster" as author when no one is signed in.
	if committed.Format != ConfigFormat || committed.ConfigMap != nil || author != "weavster" {
		t.Errorf("committed %+v by %q", committed, author)
	}
}

func TestGitPlanApplyDrift(t *testing.T) {
	var committed ConfigBundle
	var author string
	ports := func(err error) Config {
		return Config{
			Git: fakeGit{committed: &committed, author: &author, err: err}, ConfigPlanner: fakePlanner{}, Transfer: fakeTransfer{},
			Flows: &fakeFlows{}, Alerts: &memAlerts{alerts: map[string]Alert{}}, Items: memItems{},
			Snippets: &memSnippets{snippets: map[string]Snippet{}, libraries: map[string]SnippetLibrary{}},
		}
	}
	for _, tt := range []struct {
		name, method, path, body string
		cfg                      Config
		status                   int
		want                     string
	}{
		{"drift", http.MethodGet, "/api/v1/git/drift", "", ports(nil), http.StatusOK, `{"rev":"HEAD","commit":"c0ffee","drifted":true,"plan":{"fingerprint":"c"`},
		{"no drift", http.MethodGet, "/api/v1/git/drift?rev=same", "", ports(nil), http.StatusOK, `{"rev":"same","commit":"5a3e","drifted":false`},
		{"drift unknown rev", http.MethodGet, "/api/v1/git/drift?rev=missing", "", ports(nil), http.StatusNotFound, "not found in the repository"},
		{"drift invalid repository", http.MethodGet, "/api/v1/git/drift?rev=invalid", "", ports(nil), http.StatusBadRequest, "flows/a.yaml: bad"},
		{"drift planner rejects", http.MethodGet, "/api/v1/git/drift?rev=bad", "", ports(nil), http.StatusBadRequest, "flows.a: bad"},
		{"drift planner fails", http.MethodGet, "/api/v1/git/drift?rev=other", "", ports(nil), http.StatusInternalServerError, "internal error"},
		{"drift read fails", http.MethodGet, "/api/v1/git/drift", "", ports(errDisk), http.StatusInternalServerError, "internal error"},
		{"drift without planner", http.MethodGet, "/api/v1/git/drift", "", Config{Git: fakeGit{}}, http.StatusServiceUnavailable, "planning unavailable"},
		{"drift without git", http.MethodGet, "/api/v1/git/drift", "", func() Config { c := ports(nil); c.Git = nil; return c }(), http.StatusServiceUnavailable, "git.path"},
		{"drift without stores", http.MethodGet, "/api/v1/git/drift", "", Config{Git: fakeGit{}, ConfigPlanner: fakePlanner{}}, http.StatusServiceUnavailable, "export and import unavailable"},
		{"plan from git", http.MethodPost, "/api/v1/config/plan?gitRev=", "ignored", ports(nil), http.StatusOK, `"fingerprint":"c"`},
		{"plan from git rev", http.MethodPost, "/api/v1/config/plan?gitRev=same", "", ports(nil), http.StatusOK, `"fingerprint":"f"`},
		{"plan from unknown rev", http.MethodPost, "/api/v1/config/plan?gitRev=missing", "", ports(nil), http.StatusNotFound, "not found"},
		{"plan from git off", http.MethodPost, "/api/v1/config/plan?gitRev=", "", func() Config { c := ports(nil); c.Git = nil; return c }(), http.StatusServiceUnavailable, "git.path"},
		{"apply from git", http.MethodPost, "/api/v1/config/apply?gitRev=same&fingerprint=f&dryRun=true", "", ports(nil), http.StatusOK, `"applied":false`},
		{"apply from git stale", http.MethodPost, "/api/v1/config/apply?gitRev=same&fingerprint=old", "", ports(nil), http.StatusConflict, "plan again"},
		{"apply from invalid repository", http.MethodPost, "/api/v1/config/apply?gitRev=invalid&fingerprint=f", "", ports(nil), http.StatusBadRequest, "flows/a.yaml"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(tt.cfg).Router().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("got %d %.300s; want %d containing %q", rec.Code, rec.Body.String(), tt.status, tt.want)
			}
		})
	}
}

func TestGitRemoteHandlers(t *testing.T) {
	with := func(err error) Config { return Config{Git: fakeGit{err: err}} }
	for _, tt := range []struct {
		name, method, path string
		cfg                Config
		status             int
		want               string
	}{
		{"status", http.MethodGet, "/api/v1/git/remote", with(nil), http.StatusOK, `"remoteHead":"abc","ahead":0,"behind":0`},
		{"push", http.MethodPost, "/api/v1/git/push", with(nil), http.StatusOK, `"head":"def"`},
		{"pull", http.MethodPost, "/api/v1/git/pull", with(nil), http.StatusOK, `{"head":"abc","dropped":["def"]}`},
		{"push rejected", http.MethodPost, "/api/v1/git/push", with(fmt.Errorf("%w: moved on; pull first", ErrGitConflict)), http.StatusConflict, "pull first"},
		{"remote unreachable", http.MethodGet, "/api/v1/git/remote", with(fmt.Errorf("%w: https://example.com/r.git: authentication required", ErrGitRemote)), http.StatusBadGateway, "authentication required"},
		{"pull fails", http.MethodPost, "/api/v1/git/pull", with(errDisk), http.StatusInternalServerError, "internal error"},
		{"status not configured", http.MethodGet, "/api/v1/git/remote", Config{}, http.StatusServiceUnavailable, "git.path"},
		{"push not configured", http.MethodPost, "/api/v1/git/push", Config{}, http.StatusServiceUnavailable, "git.path"},
		{"pull not configured", http.MethodPost, "/api/v1/git/pull", Config{}, http.StatusServiceUnavailable, "git.path"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(tt.cfg).Router().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("got %d %.300s; want %d containing %q", rec.Code, rec.Body.String(), tt.status, tt.want)
			}
		})
	}
}

func TestGitDiffRestoreHandlers(t *testing.T) {
	with := func(err error) Config { return Config{Git: fakeGit{err: err}} }
	for _, tt := range []struct {
		name, method, path, body string
		cfg                      Config
		status                   int
		want                     string
	}{
		{"diff revisions", http.MethodGet, "/api/v1/git/diff?from=HEAD~1", "", with(nil), http.StatusOK, `{"from":"HEAD~1","to":"HEAD","files":[{"path":"flows/a.yaml","status":"modified"}]`},
		{"diff working tree", http.MethodGet, "/api/v1/git/diff", "", with(nil), http.StatusOK, `{"from":"","to":""`},
		{"diff to without from", http.MethodGet, "/api/v1/git/diff?to=HEAD", "", with(nil), http.StatusBadRequest, "to needs from"},
		{"diff unknown revision", http.MethodGet, "/api/v1/git/diff?from=missing", "", with(nil), http.StatusNotFound, "not found"},
		{"diff fails", http.MethodGet, "/api/v1/git/diff?from=a", "", with(errDisk), http.StatusInternalServerError, "internal error"},
		{"restore", http.MethodPost, "/api/v1/git/restore", `{"rev":"HEAD~2","path":"flows/a.yaml","message":"back"}`, with(nil), http.StatusOK, `"head":"HEAD~2:flows/a.yaml:weavster"`},
		{"restore needs rev", http.MethodPost, "/api/v1/git/restore", `{"message":"back"}`, with(nil), http.StatusBadRequest, "rev and message are required"},
		{"restore needs message", http.MethodPost, "/api/v1/git/restore", `{"rev":"HEAD~1"}`, with(nil), http.StatusBadRequest, "rev and message are required"},
		{"restore bad body", http.MethodPost, "/api/v1/git/restore", `{"revision":"x"}`, with(nil), http.StatusBadRequest, "invalid JSON body"},
		{"restore over changes", http.MethodPost, "/api/v1/git/restore", `{"rev":"dirty","message":"x"}`, with(nil), http.StatusConflict, "uncommitted"},
		{"restore fails", http.MethodPost, "/api/v1/git/restore", `{"rev":"a","message":"x"}`, with(errDisk), http.StatusInternalServerError, "internal error"},
		{"diff not configured", http.MethodGet, "/api/v1/git/diff", "", Config{}, http.StatusServiceUnavailable, "git.path"},
		{"restore not configured", http.MethodPost, "/api/v1/git/restore", `{}`, Config{}, http.StatusServiceUnavailable, "git.path"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(tt.cfg).Router().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("got %d %.300s; want %d containing %q", rec.Code, rec.Body.String(), tt.status, tt.want)
			}
		})
	}
}
