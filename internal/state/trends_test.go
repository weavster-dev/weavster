package state

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestMessageTrends(t *testing.T) {
	type trender interface {
		MessageTrends(context.Context, TrendQuery) (TrendCounts, error)
	}
	base := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	for name, backend := range testBackends(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			for i, m := range []Message{
				{FlowID: "a", Status: StatusSent, ReceivedAt: base},
				{FlowID: "a", Status: StatusErrored, ReceivedAt: base.Add(30 * time.Minute)},
				{FlowID: "b", Status: StatusSent, ReceivedAt: base.Add(90 * time.Minute)},
				{FlowID: "a", Status: StatusSent, ReceivedAt: base.Add(3 * time.Hour)}, // after To
				{FlowID: "a", Status: StatusSent, ReceivedAt: base.Add(-time.Minute)},  // before From
			} {
				m.ID = string(rune('a' + i))
				if err := backend.Put(ctx, m); err != nil {
					t.Fatal(err)
				}
			}
			tr := backend.(trender)
			for _, tt := range []struct {
				flow string
				want TrendCounts
			}{
				{"", TrendCounts{0: {StatusSent: 1, StatusErrored: 1}, 1: {StatusSent: 1}}},
				{"a", TrendCounts{0: {StatusSent: 1, StatusErrored: 1}}},
				{"none", TrendCounts{}},
			} {
				got, err := tr.MessageTrends(ctx, TrendQuery{FlowID: tt.flow, From: base, To: base.Add(3 * time.Hour), Bucket: time.Hour})
				if err != nil || !reflect.DeepEqual(got, tt.want) {
					t.Errorf("flow %q = %v, %v; want %v", tt.flow, got, err, tt.want)
				}
			}
		})
	}
}
