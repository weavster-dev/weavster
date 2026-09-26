package state

import (
	"context"
	"errors"
	"testing"
)

func TestFlowStore(t *testing.T) {
	ctx := context.Background()
	backends := map[string]func(t *testing.T) Store{
		"memory": func(*testing.T) Store { return NewMemStore() },
		"sqlite": func(t *testing.T) Store {
			s, err := OpenSQLite(ctx, ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			return s
		},
	}
	for name, open := range backends {
		t.Run(name, func(t *testing.T) {
			s := open(t)
			defer func() { _ = s.Close() }()

			if flows, err := s.ListFlows(ctx); err != nil || len(flows) != 0 {
				t.Fatalf("empty ListFlows = %v, %v", flows, err)
			}
			for _, f := range []FlowDefinition{{ID: "b", Document: []byte(`{"v":1}`)}, {ID: "a", Document: []byte(`{"v":2}`)}} {
				if err := s.PutFlow(ctx, f); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.PutFlow(ctx, FlowDefinition{ID: "b", Document: []byte(`{"v":3}`)}); err != nil {
				t.Fatal(err)
			}
			got, err := s.GetFlow(ctx, "b")
			if err != nil || string(got.Document) != `{"v":3}` || got.UpdatedAt.IsZero() {
				t.Errorf("GetFlow(b) = %+v, %v; want replaced document", got, err)
			}
			flows, err := s.ListFlows(ctx)
			if err != nil || len(flows) != 2 || flows[0].ID != "a" || flows[1].ID != "b" {
				t.Errorf("ListFlows = %+v, %v; want [a b]", flows, err)
			}
			if err := s.DeleteFlow(ctx, "a"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.GetFlow(ctx, "a"); !errors.Is(err, ErrFlowNotFound) {
				t.Errorf("GetFlow deleted = %v, want ErrFlowNotFound", err)
			}
			if err := s.DeleteFlow(ctx, "a"); !errors.Is(err, ErrFlowNotFound) {
				t.Errorf("DeleteFlow missing = %v, want ErrFlowNotFound", err)
			}
		})
	}
}

func TestFlowStoreClosedDB(t *testing.T) {
	ctx := context.Background()
	s, err := OpenSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if err := s.PutFlow(ctx, FlowDefinition{ID: "x"}); err == nil {
		t.Error("PutFlow on closed db: want error")
	}
	if _, err := s.GetFlow(ctx, "x"); err == nil || errors.Is(err, ErrFlowNotFound) {
		t.Errorf("GetFlow on closed db = %v, want driver error", err)
	}
	if _, err := s.ListFlows(ctx); err == nil {
		t.Error("ListFlows on closed db: want error")
	}
	if err := s.DeleteFlow(ctx, "x"); err == nil || errors.Is(err, ErrFlowNotFound) {
		t.Errorf("DeleteFlow on closed db = %v, want driver error", err)
	}
}
