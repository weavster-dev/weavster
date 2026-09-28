package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
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
	q, ok := messageQuery(w, r, maxMessageLimit)
	if !ok {
		return
	}
	msgs, err := s.cfg.Messages.Search(r.Context(), q)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	ids := make([]string, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	s.auditDisclosed(r, ids)
	// The total is a second query: when it fails the page is still
	// answered, without X-Total-Count.
	if total, err := s.cfg.Messages.Count(r.Context(), q); err == nil {
		w.Header().Set("X-Total-Count", strconv.Itoa(total))
	}
	writeJSON(w, http.StatusOK, msgs)
}

// Search filter limits.
const (
	maxMetadataFilters = 10
	maxAttemptsFilter  = 1000
)

// messageQuery reads the search parameters (limit up to maxLimit); on a bad
// value it answers 400 and returns false.
func messageQuery(w http.ResponseWriter, r *http.Request, maxLimit int) (MessageQuery, bool) {
	v := r.URL.Query()
	q := MessageQuery{Status: v.Get("status"), FlowID: v.Get("flowId"), Limit: defaultMessageLimit, Sort: "-receivedAt"}
	bad := func(msg string) (MessageQuery, bool) {
		writeStatusError(w, http.StatusBadRequest, msg)
		return q, false
	}
	if msg := timeRange(v, &q.From, &q.To); msg != "" {
		return bad(msg)
	}
	if raw := v.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxLimit {
			return bad(fmt.Sprintf("limit must be between 1 and %d", maxLimit))
		}
		q.Limit = n
	}
	if raw := v.Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return bad("offset must be 0 or more")
		}
		q.Offset = n
	}
	q.IDFrom, q.IDTo, q.ContentType = v.Get("idFrom"), v.Get("idTo"), v.Get("contentType")
	if q.IDFrom != "" && q.IDTo != "" && q.IDFrom > q.IDTo {
		return bad("idFrom must not be after idTo")
	}
	for _, p := range []struct {
		name string
		n    *int
	}{{"minAttempts", &q.MinAttempts}, {"maxAttempts", &q.MaxAttempts}} {
		if raw := v.Get(p.name); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > maxAttemptsFilter {
				return bad(fmt.Sprintf("%s must be between 1 and %d", p.name, maxAttemptsFilter))
			}
			*p.n = n
		}
	}
	if q.MinAttempts > 0 && q.MaxAttempts > 0 && q.MinAttempts > q.MaxAttempts {
		return bad("minAttempts must not be more than maxAttempts")
	}
	var names []string
	for name := range v {
		if strings.HasPrefix(name, "metadata.") {
			names = append(names, name)
		}
	}
	if len(names) > maxMetadataFilters {
		return bad(fmt.Sprintf("at most %d metadata filters", maxMetadataFilters))
	}
	sort.Strings(names) // the same parameter is named on every request
	for _, name := range names {
		key := strings.TrimPrefix(name, "metadata.")
		if key == "" || len(v[name]) != 1 {
			return bad(fmt.Sprintf("%s must name a metadata key and be given once", name))
		}
		if q.Metadata == nil {
			q.Metadata = map[string]string{}
		}
		q.Metadata[key] = v[name][0]
	}
	if raw := v.Get("sort"); raw != "" {
		switch raw {
		case "receivedAt", "-receivedAt", "id", "-id":
			q.Sort = raw
		default:
			return bad("sort must be receivedAt, -receivedAt, id, or -id")
		}
	}
	return q, true
}

// timeRange reads the optional from and to query parameters (RFC 3339)
// into from and to; it returns what is wrong with them, or "".
func timeRange(v url.Values, from, to *time.Time) string {
	for _, p := range []struct {
		name string
		t    *time.Time
	}{{"from", from}, {"to", to}} {
		if raw := v.Get(p.name); raw != "" {
			// An unencoded "+" in an offset arrives as a space.
			parsed, err := time.Parse(time.RFC3339, strings.ReplaceAll(raw, " ", "+"))
			if err != nil {
				return p.name + " must be an RFC 3339 time, for example 2026-09-26T12:00:00Z"
			}
			*p.t = parsed
		}
	}
	if !from.IsZero() && !to.IsZero() && from.After(*to) {
		return "from must not be after to"
	}
	return ""
}

// Archive limits and header.
const (
	maxExportLimit   = 10000
	maxArchiveBytes  = 100 << 20
	archiveKeyHeader = "Weavster-Archive-Key"
)

// archiveKey reads the optional encryption key: base64 of 32 bytes. On a
// bad value it answers 400 and returns false.
func archiveKey(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	raw := r.Header.Get(archiveKeyHeader)
	if raw == "" {
		return nil, true
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != 32 {
		writeStatusError(w, http.StatusBadRequest, archiveKeyHeader+" must be base64 of 32 bytes (openssl rand -base64 32)")
		return nil, false
	}
	return key, true
}

func (s *Server) handleMessagesExport(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Messages == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "messages unavailable")
		return
	}
	q, ok := messageQuery(w, r, maxExportLimit)
	if !ok {
		return
	}
	if r.URL.Query().Get("limit") == "" {
		q.Limit = maxExportLimit // an export takes as many as one archive holds
	}
	key, ok := archiveKey(w, r)
	if !ok {
		return
	}
	archive, ids, err := s.cfg.Messages.Export(r.Context(), q, key)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	if key != nil {
		w.Header().Set("Content-Type", "application/octet-stream") // encrypted
		w.Header().Set("Content-Disposition", `attachment; filename="messages.json.gz.enc"`)
	} else {
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", `attachment; filename="messages.json.gz"`)
	}
	s.auditDisclosed(r, ids)
	w.Header().Set("Weavster-Message-Count", strconv.Itoa(len(ids)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(archive)
}

func (s *Server) handleMessagesImport(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Messages == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "messages unavailable")
		return
	}
	opts := MessageImport{FlowID: r.URL.Query().Get("flowId")}
	if v := r.URL.Query().Get("overwrite"); v != "" {
		var err error
		if opts.Overwrite, err = strconv.ParseBool(v); err != nil {
			writeStatusError(w, http.StatusBadRequest, "overwrite must be true or false")
			return
		}
	}
	var ok bool
	if opts.Key, ok = archiveKey(w, r); !ok {
		return
	}
	archive, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxArchiveBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeStatusError(w, http.StatusRequestEntityTooLarge, "archive larger than 100 MiB")
			return
		}
		writeStatusError(w, http.StatusBadRequest, "could not read the archive")
		return
	}
	res, err := s.cfg.Messages.Import(r.Context(), archive, opts)
	switch {
	case errors.Is(err, ErrMessageImportIncomplete):
		writeErrorWith(w, http.StatusInternalServerError, "IMPORT_INCOMPLETE", "message import stopped part-way; imported, skipped, and busy count what was done", map[string]any{
			"imported": res.Imported, "skipped": res.Skipped, "busy": res.Busy,
		})
	case errors.Is(err, ErrFlowNotFound):
		writeStatusError(w, http.StatusNotFound, err.Error()) // names the flow
	case err != nil:
		writeFlowError(w, err)
	default:
		writeJSON(w, http.StatusOK, res)
	}
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
	s.auditDetail(r, "part", part) // disclosed
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
	id, err := r.PathValue("id"), error(nil)
	switch status, ok := r.URL.Query()["status"]; {
	case !ok:
		err = s.cfg.Messages.Delete(r.Context(), id)
	case len(status) != 1 || status[0] != "dead-lettered":
		writeStatusError(w, http.StatusBadRequest, "status can only be dead-lettered")
		return
	case !s.deadLettersAvailable(w):
		return
	default: // delete only if still dead-lettered, checked atomically
		err = s.cfg.DeadLetters.Remove(r.Context(), id)
	}
	if err != nil {
		writeFlowError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleMessagesDelete removes every message matching the search filters
// (spec §2.6.22). Without a filter it needs all=true. With restart=true the
// started flows in scope are stopped first and started again afterwards.
func (s *Server) handleMessagesDelete(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Messages == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "messages unavailable")
		return
	}
	v := r.URL.Query()
	for _, p := range []string{"limit", "offset", "sort"} {
		if v.Has(p) {
			writeStatusError(w, http.StatusBadRequest, p+" does not apply to removal; every match is removed")
			return
		}
	}
	q, ok := messageQuery(w, r, maxExportLimit)
	if !ok {
		return
	}
	all, restart := v.Get("all"), v.Get("restart")
	for _, p := range [][2]string{{"all", all}, {"restart", restart}} {
		if p[1] != "" && p[1] != "true" && p[1] != "false" {
			writeStatusError(w, http.StatusBadRequest, p[0]+" must be true or false")
			return
		}
	}
	if restart == "true" && (s.cfg.Flows == nil || s.cfg.Lifecycle == nil) {
		writeStatusError(w, http.StatusServiceUnavailable, "flow lifecycle unavailable")
		return
	}
	if !q.HasFilter() && all != "true" {
		writeStatusError(w, http.StatusBadRequest, "give a filter (the search filters: flowId, status, from, to, idFrom, idTo, contentType, minAttempts, maxAttempts, metadata.KEY), or all=true to remove every message")
		return
	}
	res := MessagesDeleted{Restarted: []string{}}
	var stopErr, deleteErr error
	if restart == "true" {
		res.Restarted, stopErr = s.stopStartedFlows(r, q.FlowID)
	}
	if stopErr == nil {
		res.Deleted, res.Busy, deleteErr = s.cfg.Messages.DeleteMatching(r.Context(), q)
	}
	// Start the stopped flows again even if the client has gone away.
	ctx := context.WithoutCancel(r.Context())
	var notStarted []string
	for _, id := range res.Restarted {
		if _, err := s.cfg.Lifecycle.Transition(ctx, id, "start"); err != nil {
			notStarted = append(notStarted, id)
		}
	}
	switch {
	case len(notStarted) > 0:
		// Say plainly which flows are still stopped, whatever else failed.
		writeStatusError(w, http.StatusInternalServerError, fmt.Sprintf(
			"removed %d messages, but these flows are still stopped; start them with POST /api/v1/flows/{id}/start: %s",
			res.Deleted, strings.Join(notStarted, ", ")))
	case stopErr != nil:
		writeFlowError(w, stopErr)
	case deleteErr != nil:
		writeStatusError(w, http.StatusInternalServerError, fmt.Sprintf(
			"removal stopped after %d messages because of an internal error; run it again", res.Deleted))
	default:
		writeJSON(w, http.StatusOK, res)
	}
}

// stopStartedFlows stops every started flow (only flowID when set) and
// returns the ones it stopped, also when it fails part-way.
func (s *Server) stopStartedFlows(r *http.Request, flowID string) ([]string, error) {
	stopped := []string{}
	flows, err := s.cfg.Flows.List(r.Context())
	if err != nil {
		return stopped, err
	}
	for _, f := range flows {
		if f.Status != "started" || (flowID != "" && f.ID != flowID) {
			continue
		}
		if _, err := s.cfg.Lifecycle.Transition(r.Context(), f.ID, "stop"); err != nil {
			return stopped, err
		}
		stopped = append(stopped, f.ID)
	}
	return stopped, nil
}

func (s *Server) handleMessageReprocess(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Messages == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "messages unavailable")
		return
	}
	res, err := s.cfg.Messages.Reprocess(r.Context(), r.PathValue("id"))
	writeIngestResult(w, res, err)
}

// auditDisclosed records which messages a read disclosed (PHI access):
// messages, how many, and messages.ids, every id as a JSON array (so an id
// holding a comma stays one id).
func (s *Server) auditDisclosed(r *http.Request, ids []string) {
	list, _ := json.Marshal(ids) // strings always marshal
	s.auditDetail(r, "messages", strconv.Itoa(len(ids)), "messages.ids", string(list))
}
