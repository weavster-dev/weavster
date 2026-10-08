package state

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestConfigItems(t *testing.T) {
	type itemStore interface {
		ListItems(context.Context, string) (map[string]json.RawMessage, error)
		GetItem(context.Context, string, string) (json.RawMessage, error)
		PutItem(context.Context, string, string, json.RawMessage) error
		DeleteItem(context.Context, string, string) error
		ReplaceItems(context.Context, string, map[string]json.RawMessage) error
		PutItems(context.Context, string, map[string]json.RawMessage) error
	}
	for name, backend := range testBackends(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := backend.(itemStore)
			steps := []struct {
				name string
				do   func() error
			}{
				{"put", func() error { return s.PutItem(ctx, "map", "region", json.RawMessage(`"eu"`)) }},
				{"put other kind", func() error { return s.PutItem(ctx, "settings", "region", json.RawMessage(`5`)) }},
				{"replace existing", func() error { return s.PutItem(ctx, "map", "region", json.RawMessage(`"us"`)) }},
				{"delete missing", func() error {
					if err := s.DeleteItem(ctx, "map", "nope"); !errors.Is(err, ErrItemNotFound) {
						return errors.New("want ErrItemNotFound")
					}
					return nil
				}},
			}
			for _, st := range steps {
				if err := st.do(); err != nil {
					t.Fatalf("%s: %v", st.name, err)
				}
			}
			if v, err := s.GetItem(ctx, "map", "region"); err != nil || string(v) != `"us"` {
				t.Errorf("get = %s, %v", v, err)
			}
			if _, err := s.GetItem(ctx, "map", "nope"); !errors.Is(err, ErrItemNotFound) {
				t.Errorf("get missing = %v", err)
			}
			if err := s.ReplaceItems(ctx, "map", map[string]json.RawMessage{"a": json.RawMessage(`"1"`), "b": json.RawMessage(`"2"`)}); err != nil {
				t.Fatal(err)
			}
			all, _ := s.ListItems(ctx, "map")
			if len(all) != 2 || string(all["a"]) != `"1"` {
				t.Errorf("after replace = %v", all)
			}
			if other, _ := s.ListItems(ctx, "settings"); string(other["region"]) != `5` {
				t.Errorf("another kind changed: %v", other)
			}
			if err := s.DeleteItem(ctx, "map", "a"); err != nil {
				t.Fatal(err)
			}
			if all, _ := s.ListItems(ctx, "map"); len(all) != 1 {
				t.Errorf("after delete = %v", all)
			}
			if err := s.PutItems(ctx, "map", map[string]json.RawMessage{"b": json.RawMessage(`"3"`), "c": json.RawMessage(`"4"`)}); err != nil {
				t.Fatal(err)
			}
			if all, _ := s.ListItems(ctx, "map"); len(all) != 2 || string(all["b"]) != `"3"` || string(all["c"]) != `"4"` {
				t.Errorf("after put items = %v", all)
			}
			if err := s.PutItems(ctx, "fresh", map[string]json.RawMessage{"x": json.RawMessage(`1`)}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
