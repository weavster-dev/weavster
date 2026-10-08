package gateway

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// MessageTrendQuery selects messages received in [From, To), of FlowID when
// set, in buckets of Interval starting at From.
type MessageTrendQuery struct {
	FlowID   string
	From, To time.Time
	Interval time.Duration
}

// MessageTrendReader counts messages per bucket index and status (spec §5
// message trends); buckets without messages may be missing.
type MessageTrendReader interface {
	MessageTrends(ctx context.Context, q MessageTrendQuery) (map[int]map[string]int, error)
}

// TrendBucket is one time bucket: messages received in it, by current status.
type TrendBucket struct {
	Start    time.Time      `json:"start"`
	Total    int            `json:"total"`
	Statuses map[string]int `json:"statuses"`
}

// messageStatuses are listed in every bucket (zero when none); any other
// status the store reports is listed and counted too.
var messageStatuses = []string{"received", "transformed", "queued", "sent", "filtered", "errored", "dead-lettered"}

// trendIntervals are the bucket sizes; maxTrendBuckets bounds a request.
var trendIntervals = map[string]time.Duration{"hour": time.Hour, "day": 24 * time.Hour}

const maxTrendBuckets = 1000

func (s *Server) handleMessageTrends(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Trends == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "message trends unavailable")
		return
	}
	v := r.URL.Query()
	q := MessageTrendQuery{FlowID: v.Get("flowId")}
	if msg := timeRange(v, &q.From, &q.To); msg != "" {
		writeStatusError(w, http.StatusBadRequest, msg)
		return
	}
	// The store keeps receive times in milliseconds.
	q.From, q.To = q.From.Truncate(time.Millisecond), q.To.Truncate(time.Millisecond)
	if q.From.IsZero() || q.To.IsZero() || !q.To.After(q.From) {
		writeStatusError(w, http.StatusBadRequest, "from and to are required, and to must be after from")
		return
	}
	name := v.Get("interval")
	if name == "" {
		name = "hour"
	}
	var ok bool
	if q.Interval, ok = trendIntervals[name]; !ok {
		writeStatusError(w, http.StatusBadRequest, "interval must be hour or day")
		return
	}
	// Compare before dividing: a range of centuries overflows a Duration sum.
	if span := q.To.Sub(q.From); span > time.Duration(maxTrendBuckets)*q.Interval {
		writeStatusError(w, http.StatusBadRequest, fmt.Sprintf("the range holds more than %d buckets (use a larger interval or a shorter range)", maxTrendBuckets))
		return
	}
	n := int((q.To.Sub(q.From) + q.Interval - 1) / q.Interval) // buckets, the last one may be shorter
	if q.FlowID != "" {
		// An unknown flow is 404, never a series of zeros.
		if s.cfg.Flows == nil {
			writeStatusError(w, http.StatusServiceUnavailable, "flows unavailable")
			return
		}
		if _, err := s.cfg.Flows.Get(r.Context(), q.FlowID); err != nil {
			writeFlowError(w, err)
			return
		}
	}
	counts, err := s.cfg.Trends.MessageTrends(r.Context(), q)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	out := make([]TrendBucket, n)
	for i := range out {
		b := TrendBucket{Start: q.From.Add(time.Duration(i) * q.Interval).UTC(), Statuses: map[string]int{}}
		for _, st := range messageStatuses {
			b.Statuses[st] = 0
		}
		for st, c := range counts[i] {
			b.Statuses[st] = c
			b.Total += c
		}
		out[i] = b
	}
	writeJSON(w, http.StatusOK, out)
}
