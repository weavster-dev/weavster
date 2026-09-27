package gateway

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// GitInfo describes the server's Git repository.
type GitInfo struct {
	Branch string `json:"branch"`
	Head   string `json:"head"` // "" before the first commit
}

// GitRevision is one commit.
type GitRevision struct {
	Hash    string    `json:"hash"`
	Message string    `json:"message"`
	Author  string    `json:"author"`
	At      time.Time `json:"at"`
}

// GitCommitResult reports a commit of the live configuration.
type GitCommitResult struct {
	Committed bool     `json:"committed"` // false: the repository already matched
	Head      string   `json:"head"`
	Changed   []string `json:"changed"` // files added, updated, or removed
}

// GitRepository is the server's Git repository of configuration (spec
// §2.10.34, #107 D-51).
type GitRepository interface {
	GitInfo(ctx context.Context) (GitInfo, error)
	// GitCommit writes live into the repository and commits it as author.
	GitCommit(ctx context.Context, live ConfigBundle, message, author string) (GitCommitResult, error)
	// GitLog lists the newest limit commits, newest first; with path only
	// those that changed it.
	GitLog(ctx context.Context, path string, limit int) ([]GitRevision, error)
	// GitContent returns path at rev (ErrGitNotFound when either does not
	// exist).
	GitContent(ctx context.Context, path, rev string) ([]byte, error)
	// GitDocument returns the repository's configuration at rev as one
	// config document, with the commit hash it was read from
	// (ErrGitNotFound for an unknown revision, ErrInvalidConfig naming a
	// bad file).
	GitDocument(ctx context.Context, rev string) (doc []byte, commit string, err error)
	// GitRemote fetches and compares the branch with the remote.
	GitRemote(ctx context.Context) (GitRemoteStatus, error)
	// GitPush pushes the branch (ErrGitConflict when the remote moved on).
	GitPush(ctx context.Context) (GitRemoteStatus, error)
	// GitPull makes the branch and working tree match the remote's: the
	// remote wins.
	GitPull(ctx context.Context) (GitPullResult, error)
}

// GitRemoteStatus compares the repository's branch with the remote's.
type GitRemoteStatus struct {
	URL        string `json:"url"`
	Branch     string `json:"branch"`
	Head       string `json:"head"`
	RemoteHead string `json:"remoteHead"` // "" when the remote has no such branch
	Ahead      int    `json:"ahead"`      // local commits the remote lacks
	Behind     int    `json:"behind"`     // remote commits missing locally
}

// GitPullResult reports a pull.
type GitPullResult struct {
	Head    string   `json:"head"`
	Dropped []string `json:"dropped"` // local commits not on the remote, now gone
}

// ErrGitNotFound: the revision or file is not in the repository.
var ErrGitNotFound = errors.New("revision or file not found in the repository")

// ErrGitConflict: the remote operation cannot be done as asked (no remote,
// the remote moved on, nothing to push); the message says why.
var ErrGitConflict = errors.New("git")

// ErrGitRemote: the remote could not be reached or refused the request;
// the message says why (never the credentials).
var ErrGitRemote = errors.New("git remote")

// ErrGitNameClash: two artifacts would share a repository file on a
// case-insensitive filesystem.
var ErrGitNameClash = errors.New("artifact names differ only in case; rename one to commit")

// Git log limits.
const (
	DefaultGitLogLimit = 100
	MaxGitLogLimit     = 1000
)

func (s *Server) gitAvailable(w http.ResponseWriter) bool {
	if s.cfg.Git == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "git repository not configured (server config git.path)")
		return false
	}
	return true
}

func writeGitError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrGitNotFound):
		writeStatusError(w, http.StatusNotFound, err.Error())
		return
	case errors.Is(err, ErrGitNameClash), errors.Is(err, ErrGitConflict):
		writeStatusError(w, http.StatusConflict, err.Error())
		return
	case errors.Is(err, ErrGitRemote):
		writeStatusError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeBackendError(w, err)
}

func (s *Server) handleGitInfo(w http.ResponseWriter, r *http.Request) {
	if !s.gitAvailable(w) {
		return
	}
	info, err := s.cfg.Git.GitInfo(r.Context())
	if err != nil {
		writeGitError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// handleGitCommit commits the live configuration (without the config map)
// as the authenticated user. Commits run one at a time, each reading the
// configuration it commits, so an older snapshot never lands on a newer.
func (s *Server) handleGitCommit(w http.ResponseWriter, r *http.Request) {
	if !s.gitAvailable(w) || !s.configPorts(w, false, false) {
		return
	}
	var req struct {
		Message string `json:"message"`
	}
	if !readStrictJSON(w, r, 1<<20, &req) {
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		writeStatusError(w, http.StatusBadRequest, "message is required")
		return
	}
	s.gitMu.Lock()
	defer s.gitMu.Unlock()
	live, err := s.liveConfig(r.Context(), false)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	author := "weavster"
	if id, ok := IdentityFrom(r.Context()); ok {
		author = id.Username
	}
	res, err := s.cfg.Git.GitCommit(r.Context(), live, req.Message, author)
	if err != nil {
		writeGitError(w, err)
		return
	}
	s.auditGit(r, "git.head", res.Head, "git.changed", strconv.Itoa(len(res.Changed)))
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleGitLog(w http.ResponseWriter, r *http.Request) {
	if !s.gitAvailable(w) {
		return
	}
	v := r.URL.Query()
	limit := DefaultGitLogLimit
	if raw := v.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > MaxGitLogLimit {
			writeStatusError(w, http.StatusBadRequest, "limit must be between 1 and 1000")
			return
		}
		limit = n
	}
	revs, err := s.cfg.Git.GitLog(r.Context(), v.Get("path"), limit)
	if err != nil {
		writeGitError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, revs)
}

// handleGitContent sends a file as it is stored, never converted:
// configuration files as YAML, anything else by its content.
func (s *Server) handleGitContent(w http.ResponseWriter, r *http.Request) {
	if !s.gitAvailable(w) {
		return
	}
	v := r.URL.Query()
	path, rev := v.Get("path"), v.Get("rev")
	if path == "" {
		writeStatusError(w, http.StatusBadRequest, "path is required")
		return
	}
	if rev == "" {
		rev = "HEAD"
	}
	content, err := s.cfg.Git.GitContent(r.Context(), path, rev)
	if err != nil {
		writeGitError(w, err)
		return
	}
	ctype := http.DetectContentType(content)
	if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
		ctype = "application/yaml"
	}
	w.Header().Set("Content-Type", ctype)
	_, _ = w.Write(content)
}

// configDocument returns the config document a plan or apply works on: the
// repository's at gitRev when that parameter is present (empty = HEAD,
// #107 D-52), otherwise the request body. It answers the error and returns
// false on failure.
func (s *Server) configDocument(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	q := r.URL.Query()
	if !q.Has("gitRev") {
		return readConfigBody(w, r)
	}
	doc, rev, commit, ok := s.gitDocument(w, r, q.Get("gitRev"))
	if ok { // the audit record names what was planned or applied
		s.auditGit(r, "git.rev", rev, "git.commit", commit)
	}
	return doc, ok
}

// gitDocument reads the repository's document at rev (empty = HEAD),
// returning the revision and the commit it resolved to.
func (s *Server) gitDocument(w http.ResponseWriter, r *http.Request, rev string) (doc []byte, _, commit string, ok bool) {
	if !s.gitAvailable(w) {
		return nil, "", "", false
	}
	if rev == "" {
		rev = "HEAD"
	}
	doc, commit, err := s.cfg.Git.GitDocument(r.Context(), rev)
	if errors.Is(err, ErrInvalidConfig) {
		writeStatusError(w, http.StatusBadRequest, err.Error())
		return nil, "", "", false
	}
	if err != nil {
		writeGitError(w, err)
		return nil, "", "", false
	}
	return doc, rev, commit, true
}

// GitDrift reports whether the live configuration differs from the
// repository at Rev, and how.
type GitDrift struct {
	Rev     string     `json:"rev"`
	Commit  string     `json:"commit"` // the commit Rev resolved to
	Drifted bool       `json:"drifted"`
	Plan    ConfigPlan `json:"plan"`
}

// handleGitDrift plans the repository's configuration against the live one
// without changing anything (on-demand drift detection, #107 D-52).
func (s *Server) handleGitDrift(w http.ResponseWriter, r *http.Request) {
	if s.cfg.ConfigPlanner == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "configuration planning unavailable")
		return
	}
	if !s.configPorts(w, false, false) {
		return
	}
	doc, rev, commit, ok := s.gitDocument(w, r, r.URL.Query().Get("rev"))
	if !ok {
		return
	}
	live, err := s.liveConfig(r.Context(), true)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	plan, err := s.cfg.ConfigPlanner.PlanConfig(doc, live)
	if errors.Is(err, ErrInvalidConfig) {
		writeStatusError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, GitDrift{Rev: rev, Commit: commit, Drifted: len(plan.Changes) > 0, Plan: plan})
}

func (s *Server) handleGitRemote(w http.ResponseWriter, r *http.Request) {
	if !s.gitAvailable(w) {
		return
	}
	st, err := s.cfg.Git.GitRemote(r.Context())
	if err != nil {
		writeGitError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleGitPush(w http.ResponseWriter, r *http.Request) {
	if !s.gitAvailable(w) {
		return
	}
	st, err := s.cfg.Git.GitPush(r.Context())
	if err != nil {
		writeGitError(w, err)
		return
	}
	s.auditGit(r, "git.head", st.Head)
	writeJSON(w, http.StatusOK, st)
}

// handleGitPull resets the repository to the remote (remote wins); the
// live configuration does not change.
func (s *Server) handleGitPull(w http.ResponseWriter, r *http.Request) {
	if !s.gitAvailable(w) {
		return
	}
	s.gitMu.Lock() // not while a commit is writing the working tree
	defer s.gitMu.Unlock()
	res, err := s.cfg.Git.GitPull(r.Context())
	if err != nil {
		writeGitError(w, err)
		return
	}
	s.auditGit(r, "git.head", res.Head, "git.dropped", strings.Join(res.Dropped, ","))
	writeJSON(w, http.StatusOK, res)
}

// auditGit adds key/value pairs to the request's audit record.
func (s *Server) auditGit(r *http.Request, kv ...string) {
	info := auditInfoFrom(r.Context())
	if info.detail == nil {
		info.detail = map[string]string{}
	}
	for i := 0; i+1 < len(kv); i += 2 {
		info.detail[kv[i]] = kv[i+1]
	}
}
