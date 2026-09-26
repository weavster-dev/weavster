package state

import (
	"context"
	"errors"
	"testing"
)

func TestFlowStore(t *testing.T) {
	ctx := context.Background()
	type flowStore interface {
		Store
		CreateFlow(context.Context, FlowDefinition) error
		GetFlow(context.Context, string) (FlowDefinition, error)
		ListFlows(context.Context) ([]FlowDefinition, error)
		DeleteFlow(context.Context, string) error
	}
	backends := map[string]func(t *testing.T) flowStore{
		"memory": func(*testing.T) flowStore { return NewMemStore() },
		"sqlite": func(t *testing.T) flowStore {
			s, err := OpenSQLite(ctx, ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			return s.(flowStore)
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
				if err := s.CreateFlow(ctx, f); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.CreateFlow(ctx, FlowDefinition{ID: "b", Document: []byte(`{"v":3}`)}); !errors.Is(err, ErrFlowExists) {
				t.Errorf("duplicate CreateFlow = %v, want ErrFlowExists", err)
			}
			got, err := s.GetFlow(ctx, "b")
			if err != nil || string(got.Document) != `{"v":1}` {
				t.Errorf("GetFlow(b) = %+v, %v; want the original document", got, err)
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
	st, err := OpenSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	s := st.(*sqlStore)
	_ = s.Close()
	if err := s.CreateFlow(ctx, FlowDefinition{ID: "x"}); err == nil {
		t.Error("CreateFlow on closed db: want error")
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
