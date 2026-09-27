package state

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// TestSearchFilterParity runs the same queries against every backend so the
// SQL WHERE builder and the in-memory predicate stay semantically identical.
func TestSearchFilterParity(t *testing.T) {
	base := time.UnixMilli(1_700_000_000_000)
	seed := []Message{
		{ID: "a1", FlowID: "f", Status: StatusSent, ContentType: "hl7v2", ReceivedAt: base,
			Metadata: map[string]string{"env": "prod"},
			Attempts: map[string]DestinationAttempt{"http-1": {Attempts: 1}}},
		{ID: "a2", FlowID: "f", Status: StatusErrored, ContentType: "x12", ReceivedAt: base.Add(time.Hour),
			Metadata: map[string]string{"env": "test"},
			Attempts: map[string]DestinationAttempt{"http-1": {Attempts: 5}}},
		{ID: "a3", FlowID: "f", Status: StatusSent, ContentType: "hl7v2", ReceivedAt: base.Add(2 * time.Hour)},
	}

	tests := []struct {
		name  string
		query Query
		want  []string
	}{
		{name: "id range", query: Query{IDFrom: "a2", IDTo: "a3"}, want: []string{"a2", "a3"}},
		{name: "id upper bound", query: Query{IDTo: "a1"}, want: []string{"a1"}},
		{name: "received before", query: Query{To: base.Add(30 * time.Minute)}, want: []string{"a1"}},
		{name: "content type", query: Query{ContentType: "x12"}, want: []string{"a2"}},
		{name: "max attempts only", query: Query{MaxAttempts: 2}, want: []string{"a1"}},
		{name: "attempts range excludes all", query: Query{MinAttempts: 2, MaxAttempts: 4}, want: []string{}},
		{name: "metadata mismatch", query: Query{Metadata: map[string]string{"env": "stage"}}, want: []string{}},
		{name: "descending sort", query: Query{Sort: "-id"}, want: []string{"a3", "a2", "a1"}},
		{name: "offset past end", query: Query{Offset: 10}, want: []string{}},
	}

	for name, s := range testBackends(t) {
		ctx := context.Background()
		for _, m := range seed {
			if err := s.Put(ctx, m); err != nil {
				t.Fatalf("%s: Put %s: %v", name, m.ID, err)
			}
		}
		for _, tt := range tests {
			t.Run(name+"/"+tt.name, func(t *testing.T) {
				got, err := s.Search(ctx, tt.query)
				if err != nil {
					t.Fatalf("Search: %v", err)
				}
				if gotIDs := ids(got); !reflect.DeepEqual(gotIDs, tt.want) {
					t.Errorf("ids = %v, want %v", gotIDs, tt.want)
				}
			})
		}
	}
}

// TestSQLStoreClosedDB verifies that operations on a closed database return
// errors instead of panicking or silently succeeding.
func TestSQLStoreClosedDB(t *testing.T) {
	ctx := context.Background()
	s, err := OpenSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := s.Search(ctx, Query{}); err == nil {
		t.Error("Search on closed store: error = nil, want error")
	}
	if err := s.Put(ctx, Message{ID: "x", FlowID: "f", Status: StatusReceived}); err == nil {
		t.Error("Put on closed store: error = nil, want error")
	}
}

func TestSearchFlowFilterAndSortWhitelist(t *testing.T) {
	for name, s := range testBackends(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			base := time.Now().Truncate(time.Millisecond)
			for i, flow := range []string{"a", "b", "a", "a"} {
				_ = s.Put(ctx, Message{ID: fmt.Sprintf("m%d", i), FlowID: flow, Status: StatusSent, ReceivedAt: base.Add(time.Duration(i) * time.Second)})
			}
			got, err := s.Search(ctx, Query{FlowID: "a", Sort: "-received_at", Limit: 2})
			if err != nil || len(got) != 2 || got[0].ID != "m3" || got[1].ID != "m2" {
				t.Errorf("flow a newest first, limit 2 = %+v, %v", got, err)
			}
			if got, err := s.Search(ctx, Query{Sort: "id; DROP TABLE messages", Limit: 10}); err != nil || len(got) != 4 {
				t.Errorf("unknown sort field = %d results, %v; want the default order", len(got), err)
			}
		})
	}
}

// TestSearchTieBreak: messages received in the same instant page in id
// order in both directions, on every backend.
func TestSearchTieBreak(t *testing.T) {
	for name, s := range testBackends(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			at := time.UnixMilli(1_700_000_000_000)
			for _, id := range []string{"c", "a", "b"} {
				_ = s.Put(ctx, Message{ID: id, FlowID: "f", Status: StatusSent, ReceivedAt: at})
			}
			for sort, want := range map[string]string{"received_at": "abc", "-received_at": "cba"} {
				var got string
				for offset := 0; offset < 3; offset++ {
					page, err := s.Search(ctx, Query{Sort: sort, Limit: 1, Offset: offset})
					if err != nil || len(page) != 1 {
						t.Fatalf("%s offset %d: %v, %v", sort, offset, page, err)
					}
					got += page[0].ID
				}
				if got != want {
					t.Errorf("%s pages = %s, want %s", sort, got, want)
				}
			}
		})
	}
}

// TestMemStoreCopies: callers changing a message's maps never change the
// stored one.
func TestMemStoreCopies(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	m := Message{ID: "m", Metadata: map[string]string{"k": "v"}, Attempts: map[string]DestinationAttempt{"d": {Attempts: 1}}}
	_ = s.Put(ctx, m)
	m.Metadata["k"] = "changed"
	got, _ := s.Get(ctx, "m")
	got.Attempts["d"] = DestinationAttempt{Attempts: 9}
	again, _ := s.Get(ctx, "m")
	if again.Metadata["k"] != "v" || again.Attempts["d"].Attempts != 1 {
		t.Errorf("stored message changed through a copy: %+v", again)
	}
}

// TestPutKeepsReceiveTime: updating a message without a receive time keeps
// the stored one, on every backend.
func TestPutKeepsReceiveTime(t *testing.T) {
	for name, s := range testBackends(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			at := time.UnixMilli(1_700_000_000_000)
			_ = s.Put(ctx, Message{ID: "m", FlowID: "f", Status: StatusReceived, ReceivedAt: at})
			_ = s.Put(ctx, Message{ID: "m", FlowID: "f", Status: StatusSent})
			if got, _ := s.Get(ctx, "m"); !got.ReceivedAt.Equal(at) {
				t.Errorf("ReceivedAt = %v, want %v", got.ReceivedAt, at)
			}
		})
	}
}
