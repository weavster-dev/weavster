package gateway

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Message search limits.
const (
	defaultMessageLimit = 100
	maxMessageLimit     = 1000
)

func (s *Server) handleMessagesSearch(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Messages == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "messages unavailable")
		return
	}
	v := r.URL.Query()
	q := MessageQuery{Status: v.Get("status"), FlowID: v.Get("flowId"), Limit: defaultMessageLimit, Sort: "-receivedAt"}
	bad := func(msg string) { writeStatusError(w, http.StatusBadRequest, msg) }
	for _, p := range []struct {
		name string
		t    *time.Time
	}{{"from", &q.From}, {"to", &q.To}} {
		if raw := v.Get(p.name); raw != "" {
			// An unencoded "+" in an offset arrives as a space.
			parsed, err := time.Parse(time.RFC3339, strings.ReplaceAll(raw, " ", "+"))
			if err != nil {
				bad(p.name + " must be an RFC 3339 time, for example 2026-09-26T12:00:00Z")
				return
			}
			*p.t = parsed
		}
	}
	if raw := v.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxMessageLimit {
			bad(fmt.Sprintf("limit must be between 1 and %d", maxMessageLimit))
			return
		}
		q.Limit = n
	}
	if raw := v.Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			bad("offset must be 0 or more")
			return
		}
		q.Offset = n
	}
	if raw := v.Get("sort"); raw != "" {
		switch raw {
		case "receivedAt", "-receivedAt", "id", "-id":
			q.Sort = raw
		default:
			bad("sort must be receivedAt, -receivedAt, id, or -id")
			return
		}
	}
	msgs, err := s.cfg.Messages.Search(r.Context(), q)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (s *Server) handleMessageGet(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Messages == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "messages unavailable")
		return
	}
	m, err := s.cfg.Messages.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeFlowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleMessageContent(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Messages == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "messages unavailable")
		return
	}
	part := r.URL.Query().Get("part")
	if part == "" {
		part = "raw"
	}
	if part != "raw" && part != "transformed" {
		writeStatusError(w, http.StatusBadRequest, "part must be raw or transformed")
		return
	}
	c, err := s.cfg.Messages.Content(r.Context(), r.PathValue("id"), part)
	if err != nil {
		writeFlowError(w, err)
		return
	}
	w.Header().Set("Content-Type", c.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(c.Body)
}

func (s *Server) handleMessageDelete(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Messages == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "messages unavailable")
		return
	}
	if err := s.cfg.Messages.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeFlowError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMessageReprocess(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Messages == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "messages unavailable")
		return
	}
	res, err := s.cfg.Messages.Reprocess(r.Context(), r.PathValue("id"))
	if err != nil {
		writeFlowError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, res)
}
