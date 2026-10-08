package gateway

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// AuditEntry is one stored audit entry.
type AuditEntry struct {
	ID       int64             `json:"id"`
	At       time.Time         `json:"at"`
	Actor    string            `json:"actor"`
	Action   string            `json:"action"`
	Resource string            `json:"resource"`
	Detail   map[string]string `json:"detail"`
}

// AuditQuery narrows an audit search; zero fields are open. OmitReads
// leaves out entries of reads of the audit log itself.
type AuditQuery struct {
	Actor, Action, Resource string
	From, To                time.Time
	AfterID                 int64
	Limit                   int
	OmitReads               bool
}

// AuditLog reads the stored audit entries, oldest first.
type AuditLog interface {
	SearchAudit(ctx context.Context, q AuditQuery) ([]AuditEntry, error)
}

// Audit search limits.
const (
	defaultAuditLimit = 100
	maxAuditLimit     = 1000
)

func (s *Server) handleAuditSearch(w http.ResponseWriter, r *http.Request) {
	if s.cfg.AuditLog == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "audit log unavailable")
		return
	}
	v := r.URL.Query()
	q := AuditQuery{Actor: v.Get("actor"), Action: v.Get("action"), Resource: v.Get("resource"), Limit: defaultAuditLimit}
	if msg := timeRange(v, &q.From, &q.To); msg != "" {
		writeStatusError(w, http.StatusBadRequest, msg)
		return
	}
	if raw := v.Get("afterId"); raw != "" {
		n, msg := parseAfterID(raw)
		if msg != "" {
			writeStatusError(w, http.StatusBadRequest, msg)
			return
		}
		q.AfterID = n
	}
	n, msg := parseLimit(v.Get("limit"), defaultAuditLimit, maxAuditLimit)
	if msg != "" {
		writeStatusError(w, http.StatusBadRequest, msg)
		return
	}
	q.Limit = n
	// Reads of the audit log are left out unless asked for, so a collector
	// that polls sees no entries of its own polling.
	q.OmitReads = q.Action == ""
	entries, err := s.cfg.AuditLog.SearchAudit(r.Context(), q)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	if len(entries) == 0 {
		auditInfoFrom(r.Context()).action = "" // nothing disclosed: not recorded
	} else {
		s.auditDetail(r, "entries", strconv.Itoa(len(entries)))
	}
	writeJSON(w, http.StatusOK, entries)
}
