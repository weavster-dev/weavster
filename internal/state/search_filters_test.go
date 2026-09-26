package state

import (
	"context"
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
