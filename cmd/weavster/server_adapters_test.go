package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/audit"
	"github.com/weavster-dev/weavster/internal/auth"
	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/state"
)

// --- authAdapter ---

func TestAuthAdapterAuthenticate(t *testing.T) {
	p := auth.NewLocalProvider(auth.Options{
		Policy:  auth.PasswordPolicy{MinLength: 4},
		Lockout: auth.LockoutPolicy{},
	})
	ctx := context.Background()
	_ = p.CreateUser(ctx, auth.User{
		Username: "alice", PasswordHash: "pass", Permissions: []string{auth.PermAdmin},
	})

	a := authAdapter{p: p}

	id, err := a.Authenticate(ctx, "alice", "pass", "")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if id.Username != "alice" {
		t.Errorf("id.Username = %q, want alice", id.Username)
	}
	if len(id.Permissions) == 0 {
		t.Error("expected permissions to be propagated")
	}

	// Wrong password must return an error.
	if _, err := a.Authenticate(ctx, "alice", "wrong", ""); err == nil {
		t.Error("expected error for wrong password")
	}
}

// --- authorizerAdapter ---

func TestAuthorizerAdapterAuthorize(t *testing.T) {
	ctx := context.Background()
	az := authorizerAdapter{}

	cases := []struct {
		id       gateway.Identity
		resource string
		action   string
		want     bool
	}{
		{gateway.Identity{Username: "admin", Permissions: []string{auth.PermAdmin}}, "flows", "edit", true},
		{gateway.Identity{Username: "viewer", Permissions: []string{auth.PermFlowsView}}, "flows", "view", true},
		{gateway.Identity{Username: "viewer", Permissions: []string{auth.PermFlowsView}}, "flows", "edit", false},
		{gateway.Identity{}, "flows", "view", false},
	}
	for _, c := range cases {
		got := az.Authorize(ctx, c.id, c.resource, c.action)
		if got != c.want {
			t.Errorf("Authorize(%v, %q, %q) = %v, want %v", c.id, c.resource, c.action, got, c.want)
		}
	}
}

// --- auditAdapter ---

func TestAuditAdapterRecord(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sink := audit.NewLocalSink(logger)
	a := auditAdapter{s: sink}

	if err := a.Record(context.Background(), gateway.AuditEvent{Actor: "alice", Action: "create", Resource: "flow/f1"}); err != nil {
		t.Errorf("Record: %v", err)
	}
}

// --- flowAdapter ---

func TestFlowAdapter(t *testing.T) {
	ctx := context.Background()
	store := state.NewMemStore()
	a := flowAdapter{store: store}

	if flows, err := a.List(ctx); err != nil || len(flows) != 0 {
		t.Fatalf("empty List = %v, %v", flows, err)
	}
	want := gateway.Flow{ID: "f1", Name: "Flow One", SourceType: "file", Enabled: true}
	if _, err := a.Create(ctx, want); err != nil {
		t.Fatal(err)
	}
	want.Status = "undeployed" // new flows are drafts (D-29)
	if got, err := a.Get(ctx, "f1"); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Get = %+v, %v; want %+v", got, err, want)
	}
	if flows, err := a.List(ctx); err != nil || len(flows) != 1 || !reflect.DeepEqual(flows[0], want) {
		t.Errorf("List = %+v, %v", flows, err)
	}
	if _, err := a.Create(ctx, want); !errors.Is(err, gateway.ErrFlowExists) {
		t.Errorf("Create duplicate = %v, want gateway.ErrFlowExists", err)
	}
	if _, err := a.Get(ctx, "missing"); !errors.Is(err, gateway.ErrFlowNotFound) {
		t.Errorf("Get missing = %v", err)
	}

	// A corrupt stored document is reported, not silently dropped.
	if err := store.CreateFlow(ctx, state.FlowDefinition{ID: "bad", Document: []byte("{")}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Get(ctx, "bad"); err == nil {
		t.Error("Get corrupt: want error")
	}
	if _, err := a.List(ctx); err == nil {
		t.Error("List with corrupt document: want error")
	}
	if err := a.Delete(ctx, "bad"); err != nil {
		t.Fatal(err)
	}
	if err := a.Delete(ctx, "f1"); err != nil {
		t.Fatal(err)
	}
	if err := a.Delete(ctx, "f1"); !errors.Is(err, gateway.ErrFlowNotFound) {
		t.Errorf("Delete missing = %v", err)
	}
}

func TestStoresImplementFlowRepository(t *testing.T) {
	sqlite, err := state.OpenSQLite(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqlite.Close() }()
	for _, s := range []state.Store{state.NewMemStore(), sqlite} {
		if _, ok := s.(flowRepository); !ok {
			t.Errorf("%T does not implement flowRepository", s)
		}
	}
}

// --- topologyAdapter ---

func TestTopologyAdapter(t *testing.T) {
	ctx := context.Background()
	flows := flowAdapter{store: state.NewMemStore()}
	if _, err := flows.Create(ctx, gateway.Flow{ID: "admit", Name: "Patient Admit", SourceType: "file"}); err != nil {
		t.Fatal(err)
	}
	ta := topologyAdapter{flows: flows}

	if graph, err := ta.Overview(ctx); err != nil || len(graph.Nodes) != 1 {
		t.Errorf("Overview = %+v, %v; want one flow node", graph, err)
	}
	if graph, err := ta.FlowInternal(ctx, "admit"); err != nil || len(graph.Nodes) == 0 {
		t.Errorf("FlowInternal admit = %+v, %v", graph, err)
	}
	if _, err := ta.FlowInternal(ctx, "noflow"); err == nil {
		t.Error("expected error for missing flow")
	}

	broken := topologyAdapter{flows: flowAdapter{store: failingFlowStore{}}}
	if _, err := broken.Overview(ctx); err == nil {
		t.Error("Overview with failing store: want error")
	}
}

type failingFlowStore struct{ flowRepository }

func (failingFlowStore) ListFlows(context.Context) ([]state.FlowDefinition, error) {
	return nil, errors.New("store down")
}

// --- messageAdapter ---

func TestMessageAdapterSearch(t *testing.T) {
	store := state.NewMemStore()
	ma := messageAdapter{store: store}
	ctx := context.Background()

	// Empty store returns empty slice without error.
	msgs, err := ma.Search(ctx, gateway.MessageQuery{Limit: 10})
	if err != nil {
		t.Fatalf("Search empty: %v", err)
	}
	if msgs == nil {
		t.Error("expected non-nil slice")
	}

	// With a status filter.
	msgs, err = ma.Search(ctx, gateway.MessageQuery{Status: "sent", Limit: 10})
	if err != nil {
		t.Fatalf("Search with status: %v", err)
	}
	_ = msgs

	// With a flowID filter that matches nothing.
	msgs, err = ma.Search(ctx, gateway.MessageQuery{FlowID: "no-such-flow", Limit: 10})
	if err != nil {
		t.Fatalf("Search with flowID: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages for missing flow, got %d", len(msgs))
	}
}

// erroringStore is a minimal state.Store whose Search always fails, used to
// exercise messageAdapter.Search's error-propagation branch (unreachable via
// MemStore, which never errors).
type erroringStore struct{ state.Store }

func (erroringStore) Search(context.Context, state.Query) ([]state.Message, error) {
	return nil, errSearchFailed
}

var errSearchFailed = errors.New("search failed")

func TestMessageAdapterSearchError(t *testing.T) {
	ma := messageAdapter{store: erroringStore{}}
	if _, err := ma.Search(context.Background(), gateway.MessageQuery{Limit: 10}); !errors.Is(err, errSearchFailed) {
		t.Errorf("Search error = %v, want %v", err, errSearchFailed)
	}
}

func TestMessageAdapterSearchFilters(t *testing.T) {
	store := state.NewMemStore()
	ctx := context.Background()
	now := time.Now()
	for _, m := range []state.Message{
		{ID: "m1", FlowID: "admit", Status: state.StatusSent, ContentType: "hl7v2", ReceivedAt: now, UpdatedAt: now},
		{ID: "m2", FlowID: "billing", Status: state.StatusSent, ContentType: "x12", ReceivedAt: now, UpdatedAt: now},
		{ID: "m3", FlowID: "admit", Status: state.StatusErrored, ContentType: "hl7v2", ReceivedAt: now, UpdatedAt: now},
	} {
		if err := store.Put(ctx, m); err != nil {
			t.Fatalf("Put %s: %v", m.ID, err)
		}
	}
	ma := messageAdapter{store: store}

	tests := []struct {
		name    string
		query   gateway.MessageQuery
		wantIDs []string
	}{
		{name: "flow filter", query: gateway.MessageQuery{FlowID: "admit", Limit: 10}, wantIDs: []string{"m1", "m3"}},
		{name: "status and flow", query: gateway.MessageQuery{FlowID: "admit", Status: "sent", Limit: 10}, wantIDs: []string{"m1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs, err := ma.Search(ctx, tt.query)
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			got := map[string]gateway.Message{}
			for _, m := range msgs {
				got[m.ID] = m
			}
			if len(got) != len(tt.wantIDs) {
				t.Fatalf("got %d messages %v, want %v", len(got), msgs, tt.wantIDs)
			}
			for _, id := range tt.wantIDs {
				m, ok := got[id]
				if !ok {
					t.Errorf("missing message %s", id)
					continue
				}
				if m.FlowID != "admit" || m.ContentType != "hl7v2" {
					t.Errorf("message %s = %+v, fields not mapped", id, m)
				}
			}
		})
	}
}
