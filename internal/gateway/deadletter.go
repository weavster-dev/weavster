package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
)

// ErrNotDeadLettered: the message is not dead-lettered, so it cannot be
// requeued.
var ErrNotDeadLettered = errors.New("message is not dead-lettered")

// RequeueResult is a requeued message and the attempts it had before.
type RequeueResult struct {
	Message  Message                   `json:"message"`
	Previous map[string]MessageAttempt `json:"previous"`
}

// RequeueSkip is a dead-lettered message a bulk requeue left as it was.
type RequeueSkip struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// RequeueAllResult reports a bulk requeue.
type RequeueAllResult struct {
	Requeued []string      `json:"requeued"`
	Skipped  []RequeueSkip `json:"skipped"`
}

// DeadLetterRequeuer gives dead-lettered messages another round of
// delivery attempts (#107 §8).
type DeadLetterRequeuer interface {
	// Requeue requeues one dead-lettered message: ErrMessageNotFound,
	// ErrNotDeadLettered, ErrMessageBusy, or ErrFlowNotFound when its flow
	// was deleted.
	Requeue(ctx context.Context, id string) (RequeueResult, error)
	// RequeueAll requeues every dead-lettered message (of flowID when set;
	// ErrFlowNotFound for an unknown flow), skipping those it cannot.
	RequeueAll(ctx context.Context, flowID string) (RequeueAllResult, error)
}

func (s *Server) deadLettersAvailable(w http.ResponseWriter) bool {
	if s.cfg.DeadLetters == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "dead-letter requeue unavailable")
		return false
	}
	return true
}

// handleMessageRequeue requeues one dead-lettered message; the audit record
// keeps the attempts it had.
func (s *Server) handleMessageRequeue(w http.ResponseWriter, r *http.Request) {
	if !s.deadLettersAvailable(w) {
		return
	}
	res, err := s.cfg.DeadLetters.Requeue(r.Context(), r.PathValue("id"))
	if err != nil {
		writeFlowError(w, err)
		return
	}
	dests := make([]string, 0, len(res.Previous))
	for d := range res.Previous {
		dests = append(dests, d)
	}
	sort.Strings(dests)
	kv := []string{"message.id", res.Message.ID}
	for _, d := range dests {
		a := res.Previous[d]
		kv = append(kv, "previous."+d, fmt.Sprintf("attempts=%d lastError=%q", a.Attempts, a.LastError))
	}
	s.auditDetail(r, kv...)
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleMessagesRequeue(w http.ResponseWriter, r *http.Request) {
	if !s.deadLettersAvailable(w) {
		return
	}
	res, err := s.cfg.DeadLetters.RequeueAll(r.Context(), r.URL.Query().Get("flowId"))
	if err != nil {
		writeFlowError(w, err)
		return
	}
	s.auditDetail(r, "requeued", strconv.Itoa(len(res.Requeued)), "skipped", strconv.Itoa(len(res.Skipped)))
	writeJSON(w, http.StatusOK, res)
}

// auditDetail adds key/value pairs to the request's audit record.
func (s *Server) auditDetail(r *http.Request, kv ...string) {
	info := auditInfoFrom(r.Context())
	if info.detail == nil {
		info.detail = map[string]string{}
	}
	for i := 0; i+1 < len(kv); i += 2 {
		info.detail[kv[i]] = kv[i+1]
	}
}
