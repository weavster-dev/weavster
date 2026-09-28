package main

import (
	"sort"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/gateway"
)

// TestPollDue: an interval source polls at once, then each interval; a
// scheduled source first polls at the first scheduled time after it was
// seen, then at each scheduled time, in its CRON_TZ.
func TestPollDue(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, tt := range []struct {
		name  string
		src   gateway.FlowSource
		steps map[string]bool // time -> due
	}{
		{"interval", gateway.FlowSource{PollIntervalMs: 60000}, map[string]bool{
			"2026-09-28T10:00:00Z": true, "2026-09-28T10:00:30Z": false, "2026-09-28T10:01:00Z": true,
		}},
		{"default interval", gateway.FlowSource{}, map[string]bool{
			"2026-09-28T10:00:00Z": true, "2026-09-28T10:00:03Z": false, "2026-09-28T10:00:05Z": true,
		}},
		{"every five minutes", gateway.FlowSource{Schedule: "*/5 * * * *"}, map[string]bool{
			"2026-09-28T10:02:00Z": false, // first seen: waits for 10:05
			"2026-09-28T10:04:59Z": false, "2026-09-28T10:05:00Z": true, "2026-09-28T10:06:00Z": false,
			"2026-09-28T10:09:59Z": false, "2026-09-28T10:10:01Z": true,
		}},
		{"daily in Tokyo", gateway.FlowSource{Schedule: "CRON_TZ=Asia/Tokyo 0 9 * * *"}, map[string]bool{
			"2026-09-28T22:00:00Z": false,                               // 07:00 JST
			"2026-09-28T23:59:59Z": false, "2026-09-29T00:00:00Z": true, // 09:00 JST
			"2026-09-29T12:00:00Z": false,
		}},
		{"invalid schedule", gateway.FlowSource{Schedule: "not cron"}, map[string]bool{
			"2026-09-28T10:00:00Z": false, "2026-09-29T10:00:00Z": false,
		}},
	} {
		last := map[string]time.Time{}
		times := make([]string, 0, len(tt.steps))
		for k := range tt.steps {
			times = append(times, k)
		}
		sort.Strings(times) // RFC 3339 in UTC sorts in time order
		for _, ts := range times {
			if got := pollDue(&tt.src, last, "f", at(ts), 5*time.Second); got != tt.steps[ts] {
				t.Errorf("%s at %s: due = %v, want %v", tt.name, ts, got, tt.steps[ts])
			}
		}
	}
}
