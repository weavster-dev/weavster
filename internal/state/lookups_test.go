package state

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestLookups(t *testing.T) {
	type lookupStore interface {
		LookupGroups(context.Context) (map[string]int, error)
		LookupEntries(context.Context, string, string, int) (map[string]string, error)
		LookupGet(context.Context, string, []string) (map[string]string, error)
		LookupPut(context.Context, string, map[string]string, bool) error
		LookupDelete(context.Context, string, string) error
		LookupDeleteGroup(context.Context, string) error
	}
	for name, backend := range testBackends(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := backend.(lookupStore)
			if err := s.LookupPut(ctx, "mrn", map[string]string{"A-1": "x", "A-2": "y", "B%1": "z", "é1": "w"}, false); err != nil {
				t.Fatal(err)
			}
			_ = s.LookupPut(ctx, "other", map[string]string{"k": "v"}, false)
			for _, tt := range []struct {
				prefix string
				limit  int
				want   map[string]string
			}{
				{"", 0, map[string]string{"A-1": "x", "A-2": "y", "B%1": "z", "é1": "w"}},
				{"A-", 0, map[string]string{"A-1": "x", "A-2": "y"}},
				{"A-", 1, map[string]string{"A-1": "x"}},
				{"B%", 0, map[string]string{"B%1": "z"}}, // no LIKE wildcards
				{"é", 0, map[string]string{"é1": "w"}},
				{"C", 0, map[string]string{}},
			} {
				if got, err := s.LookupEntries(ctx, "mrn", tt.prefix, tt.limit); err != nil || !reflect.DeepEqual(got, tt.want) {
					t.Errorf("prefix %q limit %d = %v, %v", tt.prefix, tt.limit, got, err)
				}
			}
			if got, _ := s.LookupGet(ctx, "mrn", []string{"A-1", "nope"}); !reflect.DeepEqual(got, map[string]string{"A-1": "x"}) {
				t.Errorf("get = %v", got)
			}
			if g, _ := s.LookupGroups(ctx); !reflect.DeepEqual(g, map[string]int{"mrn": 4, "other": 1}) {
				t.Errorf("groups = %v", g)
			}
			if err := s.LookupPut(ctx, "mrn", map[string]string{"A-1": "new"}, true); err != nil {
				t.Fatal(err)
			}
			if got, _ := s.LookupEntries(ctx, "mrn", "", 0); !reflect.DeepEqual(got, map[string]string{"A-1": "new"}) {
				t.Errorf("after replace = %v", got)
			}
			if err := s.LookupDelete(ctx, "mrn", "A-1"); err != nil {
				t.Fatal(err)
			}
			if err := s.LookupDelete(ctx, "mrn", "A-1"); !errors.Is(err, ErrLookupNotFound) {
				t.Errorf("delete missing = %v", err)
			}
			if g, _ := s.LookupGroups(ctx); !reflect.DeepEqual(g, map[string]int{"other": 1}) {
				t.Errorf("empty group listed: %v", g)
			}
			if err := s.LookupDeleteGroup(ctx, "other"); err != nil {
				t.Fatal(err)
			}
			if err := s.LookupDeleteGroup(ctx, "other"); !errors.Is(err, ErrLookupNotFound) {
				t.Errorf("delete missing group = %v", err)
			}
			if err := s.LookupPut(ctx, "empty", nil, true); err != nil {
				t.Fatal(err)
			}
			if g, _ := s.LookupGroups(ctx); len(g) != 0 {
				t.Errorf("groups = %v", g)
			}
		})
	}
}
