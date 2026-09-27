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
}

// ErrGitNotFound: the revision or file is not in the repository.
var ErrGitNotFound = errors.New("revision or file not found in the repository")

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
	case errors.Is(err, ErrGitNameClash):
		writeStatusError(w, http.StatusConflict, err.Error())
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
	info := auditInfoFrom(r.Context())
	if info.detail == nil {
		info.detail = map[string]string{}
	}
	info.detail["git.head"], info.detail["git.changed"] = res.Head, strconv.Itoa(len(res.Changed))
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
