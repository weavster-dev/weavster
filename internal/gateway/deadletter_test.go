package gateway

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeRequeuer requeues "dl"; other ids and flows map to errors.
type fakeRequeuer struct{}

func (fakeRequeuer) Requeue(_ context.Context, id string) (RequeueResult, error) {
	switch id {
	case "dl":
		return RequeueResult{Message: Message{ID: "dl", Status: "queued"},
			Previous: map[string]MessageAttempt{"ehr": {Attempts: 5, LastError: "refused"}, "archive": {Attempts: 1}}}, nil
	case "sent":
		return RequeueResult{}, fmt.Errorf("%w (status sent)", ErrNotDeadLettered)
	case "busy":
		return RequeueResult{}, ErrMessageBusy
	case "gone":
		return RequeueResult{}, fmt.Errorf("%w: flow adt of message gone", ErrFlowNotFound)
	case "missing":
		return RequeueResult{}, ErrMessageNotFound
	}
	return RequeueResult{}, errDisk
}

func (fakeRequeuer) RequeueAll(_ context.Context, flowID string) (RequeueAllResult, error) {
	switch flowID {
	case "":
		return RequeueAllResult{Requeued: []string{"a", "b"}, Skipped: []RequeueSkip{{ID: "c", Reason: "message is being processed; try again"}}}, nil
	case "nope":
		return RequeueAllResult{}, ErrFlowNotFound
	}
	return RequeueAllResult{}, errDisk
}

func TestDeadLetterHandlers(t *testing.T) {
	ok := Config{DeadLetters: fakeRequeuer{}}
	for _, tt := range []struct {
		name, path string
		cfg        Config
		status     int
		want       string
	}{
		{"requeue", "/api/v1/messages/dl/requeue", ok, http.StatusOK, `"previous":{"archive":{"attempts":1},"ehr":{"attempts":5,"lastError":"refused"}}`},
		{"not dead-lettered", "/api/v1/messages/sent/requeue", ok, http.StatusConflict, "not dead-lettered (status sent)"},
		{"busy", "/api/v1/messages/busy/requeue", ok, http.StatusConflict, "being processed"},
		{"flow deleted", "/api/v1/messages/gone/requeue", ok, http.StatusNotFound, "flow not found"},
		{"unknown message", "/api/v1/messages/missing/requeue", ok, http.StatusNotFound, "message not found"},
		{"fails", "/api/v1/messages/other/requeue", ok, http.StatusInternalServerError, "internal error"},
		{"requeue all", "/api/v1/messages/requeue", ok, http.StatusOK, `{"requeued":["a","b"],"skipped":[{"id":"c","reason":"message is being processed; try again"}]}`},
		{"requeue all unknown flow", "/api/v1/messages/requeue?flowId=nope", ok, http.StatusNotFound, "flow not found"},
		{"requeue all fails", "/api/v1/messages/requeue?flowId=x", ok, http.StatusInternalServerError, "internal error"},
		{"unavailable", "/api/v1/messages/dl/requeue", Config{}, http.StatusServiceUnavailable, "dead-letter requeue unavailable"},
		{"all unavailable", "/api/v1/messages/requeue", Config{}, http.StatusServiceUnavailable, "dead-letter requeue unavailable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(tt.cfg).Router().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, tt.path, nil))
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("got %d %.300s; want %d containing %q", rec.Code, rec.Body.String(), tt.status, tt.want)
			}
		})
	}
}
