package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

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

	if err := a.Record(context.Background(), "alice", "create", "flow/f1"); err != nil {
		t.Errorf("Record: %v", err)
	}
}

// --- memFlowStore ---

func TestMemFlowStore(t *testing.T) {
	ctx := context.Background()
	s := newMemFlowStore()

	// List should return the pre-seeded "admit" flow.
	flows, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(flows) == 0 {
		t.Fatal("expected at least one seeded flow")
	}

	// Get the seeded flow.
	f, err := s.Get(ctx, "admit")
	if err != nil {
		t.Fatalf("Get admit: %v", err)
	}
	if f.ID != "admit" {
		t.Errorf("flow ID = %q", f.ID)
	}

	// Get a non-existent flow returns an error.
	if _, err := s.Get(ctx, "noflow"); err == nil {
		t.Error("expected error for missing flow")
	}

	// Create a new flow and verify it is retrievable.
	newFlow := gateway.Flow{ID: "test-flow", Name: "Test Flow", Status: "idle", Enabled: true}
	if err := s.Create(ctx, newFlow); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx, "test-flow")
	if err != nil {
		t.Fatalf("Get after Create: %v", err)
	}
	if got.Name != "Test Flow" {
		t.Errorf("created flow name = %q", got.Name)
	}

	// Delete removes the flow.
	if err := s.Delete(ctx, "test-flow"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, "test-flow"); err == nil {
		t.Error("expected error after Delete")
	}
}

// --- topologyAdapter ---

func TestTopologyAdapterOverview(t *testing.T) {
	flows := newMemFlowStore()
	ta := topologyAdapter{flows: flows}

	graph, err := ta.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if len(graph.Nodes) == 0 {
		t.Error("expected non-empty graph from Overview")
	}
}

func TestTopologyAdapterFlowInternal(t *testing.T) {
	flows := newMemFlowStore()
	ta := topologyAdapter{flows: flows}
	ctx := context.Background()

	graph, err := ta.FlowInternal(ctx, "admit")
	if err != nil {
		t.Fatalf("FlowInternal admit: %v", err)
	}
	if len(graph.Nodes) == 0 {
		t.Error("expected non-empty graph for admit flow")
	}

	// Non-existent flow must return an error.
	if _, err := ta.FlowInternal(ctx, "noflow"); err == nil {
		t.Error("expected error for missing flow")
	}
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
