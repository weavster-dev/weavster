package main

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/audit"
	"github.com/weavster-dev/weavster/internal/auth"
	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/pipeline"
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
		if _, ok := s.(itemRepository); !ok {
			t.Errorf("%T does not implement itemRepository", s)
		}
		if _, ok := s.(lookupRepository); !ok {
			t.Errorf("%T does not implement lookupRepository", s)
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

func (erroringStore) MessageTrends(context.Context, state.TrendQuery) (state.TrendCounts, error) {
	return nil, errSearchFailed
}

func TestMessageAdapterTrendsError(t *testing.T) {
	if _, err := (messageAdapter{store: erroringStore{}}).MessageTrends(context.Background(), gateway.MessageTrendQuery{Interval: time.Hour}); !errors.Is(err, errSearchFailed) {
		t.Errorf("trends error = %v", err)
	}
}

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

// TestMessageAdapterDeleteMatching pages through the matches, skips busy
// messages without losing its place, and leaves other flows alone.
func TestMessageAdapterDeleteMatching(t *testing.T) {
	defer func(n int) { deletePage = n }(deletePage)
	deletePage = 2
	ctx := context.Background()
	store := state.NewMemStore()
	for _, m := range []state.Message{{ID: "a1", FlowID: "a"}, {ID: "a2", FlowID: "a"}, {ID: "a3", FlowID: "a"}, {ID: "a4", FlowID: "a"}, {ID: "a5", FlowID: "a"}, {ID: "b1", FlowID: "b"}} {
		m.Status = state.StatusSent
		_ = store.Put(ctx, m)
	}
	pipe := pipeline.New(store, newSink, nil, pipeline.Options{})
	release, _ := pipe.Hold("a2")
	defer release()
	ma := messageAdapter{store: store, pipe: pipe}
	deleted, busy, err := ma.DeleteMatching(ctx, gateway.MessageQuery{FlowID: "a"})
	if err != nil || deleted != 4 || busy != 1 {
		t.Fatalf("DeleteMatching = %d, %d, %v; want 4, 1, nil", deleted, busy, err)
	}
	left, _ := store.Search(ctx, state.Query{Sort: "id"})
	if len(left) != 2 || left[0].ID != "a2" || left[1].ID != "b1" {
		t.Errorf("left = %+v, want a2 and b1", left)
	}
	// A message that no longer matches when it is reached is kept.
	for _, tt := range []struct {
		name string
		q    state.Query
	}{
		{"other flow", state.Query{FlowID: "a"}},
		{"other status", state.Query{Status: state.StatusQueued}},
		{"received earlier", state.Query{From: time.Now().Add(time.Hour)}},
		{"received later", state.Query{To: time.Unix(0, 0)}},
	} {
		if removed, err := ma.removeIfMatching(ctx, "b1", tt.q); removed || err != nil {
			t.Errorf("%s: removed = %v, %v; want kept", tt.name, removed, err)
		}
	}
	if removed, err := ma.removeIfMatching(ctx, "gone", state.Query{}); removed || err != nil {
		t.Errorf("missing message: removed = %v, %v", removed, err)
	}
	if _, _, err := (messageAdapter{store: erroringStore{}}).DeleteMatching(ctx, gateway.MessageQuery{}); !errors.Is(err, errSearchFailed) {
		t.Errorf("search error = %v", err)
	}
}

// flowIngest answers IngestFrom with res and err and records the call.
type flowIngest struct {
	res      gateway.IngestResult
	err      error
	target   string
	metadata map[string]string
}

func (f *flowIngest) IngestFrom(_ context.Context, flowID string, _ []byte, md map[string]string) (gateway.IngestResult, error) {
	f.target, f.metadata = flowID, md
	return f.res, f.err
}

// TestFlowSink: a flow delivery succeeds once the target stored the
// message (even if processing then failed), fails otherwise, and a retry
// that finds the message already stored for its key stores nothing new.
func TestFlowSink(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	none := func(context.Context, string, string) (string, error) { return "", nil }
	for _, tt := range []struct {
		name      string
		in        flowIngest
		delivered func(context.Context, string, string) (string, error)
		ok        bool
		ingested  bool
	}{
		{"stored", flowIngest{res: gateway.IngestResult{ID: "m2"}}, none, true, true},
		{"stored, then failed", flowIngest{res: gateway.IngestResult{ID: "m2"}, err: errors.New("disk full")}, none, true, true},
		{"not running", flowIngest{err: gateway.ErrFlowNotRunning}, none, false, true},
		{"already stored", flowIngest{}, func(_ context.Context, flow, key string) (string, error) {
			if flow == "next" && key == "k1" {
				return "m2", nil
			}
			return "", nil
		}, true, false},
		{"lookup failed", flowIngest{}, func(context.Context, string, string) (string, error) { return "", errors.New("store down") }, false, false},
	} {
		in := tt.in
		sink, _ := (&sinkFactory{ingest: &in, delivered: tt.delivered, logger: logger}).build(pipeline.Destination{Type: "flow", Flow: "next"})
		err := sink.Write(context.Background(), pipeline.Delivery{FlowID: "from", MessageID: "m1", IdempotencyKey: "k1", Body: []byte("{}")})
		if (err == nil) != tt.ok || (in.target != "") != tt.ingested {
			t.Errorf("%s: %v, ingested %v", tt.name, err, in.target != "")
		}
		if tt.ingested && (in.target != "next" || in.metadata["source.flow"] != "from" || in.metadata["source.message"] != "m1" || in.metadata[flowKeyMetadata] != "k1") {
			t.Errorf("%s: target %s, metadata %v", tt.name, in.target, in.metadata)
		}
	}
	if _, err := (&sinkFactory{}).build(pipeline.Destination{Type: "ftp"}); err == nil {
		t.Error("an unknown type built a sink")
	}
}

// TestMLLPClientTLS: an mllp destination verifies its receiver's host name,
// against caFile's certificates when set, with the server's TLS settings; an unreadable caFile or one
// without a certificate fails the delivery.
func TestMLLPClientTLS(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile, _ := selfSignedCert(t, dir)
	for _, tt := range []struct {
		name, caFile, want string
		roots              bool
	}{
		{"system roots", "", "", false},
		{"caFile", certFile, "", true},
		{"missing caFile", filepath.Join(dir, "none.pem"), "caFile:", false},
		{"caFile without a certificate", keyFile, "no PEM certificate", false},
	} {
		opts := gateway.DefaultTLSOptions()
		opts.MinVersion = tls.VersionTLS13
		cfg, err := mllpClientTLS(pipeline.Destination{Type: "mllp", Address: "lab.example:2575", TLS: true, CAFile: tt.caFile}, opts)
		switch {
		case (tt.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tt.want)):
			t.Errorf("%s: %v, want %q", tt.name, err, tt.want)
		case err == nil && (cfg.MinVersion != tls.VersionTLS13 || (cfg.RootCAs != nil) != tt.roots):
			t.Errorf("%s: MinVersion %x, roots %v", tt.name, cfg.MinVersion, cfg.RootCAs != nil)
		}
	}
	if _, err := mllpClientTLS(pipeline.Destination{Type: "mllp", Address: "lab:2575", TLS: true}, gateway.TLSOptions{}); err == nil {
		t.Error("mllpClientTLS without TLS options: want error")
	}
	if _, err := newSink(pipeline.Destination{Type: "mllp", Address: "lab:2575", TLS: true, CAFile: filepath.Join(dir, "none.pem")}); err == nil {
		t.Error("newSink with a missing caFile: want error")
	}
	if _, err := newSink(pipeline.Destination{Type: "mllp", Address: "lab:2575", TLS: true}); err != nil {
		t.Errorf("newSink over TLS: %v", err)
	}
}

// TestDBPool: one pool per driver and variable, replaced (the old one
// closed) when the variable's connection string changes, all closed on
// shutdown; a database destination needs the pool and its variable.
func TestDBPool(t *testing.T) {
	p := newDBPool()
	a, err := p.get("sqlite", "WEAVSTER_DB_A", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := p.get("sqlite", "WEAVSTER_DB_A", ":memory:"); b != a {
		t.Error("a second pool for the same variable")
	}
	if a.Stats().MaxOpenConnections != 1 {
		t.Errorf("a SQLite pool allows %d connections, want 1 (one writer)", a.Stats().MaxOpenConnections)
	}
	if c, _ := p.get("postgres", "WEAVSTER_DB_A", "postgres://x"); c == a {
		t.Error("drivers share a pool")
	}
	rotated, _ := p.get("sqlite", "WEAVSTER_DB_A", "file::memory:?x=1")
	if rotated == a || a.Ping() == nil {
		t.Error("a changed connection string did not replace and close the old pool")
	}
	p.close()
	if len(p.dbs) != 0 || rotated.Ping() == nil {
		t.Error("pools not closed")
	}
	if _, err := p.get("sqlite", "WEAVSTER_DB_A", ":memory:"); err == nil {
		t.Error("a pool opened after close")
	}
	d := pipeline.Destination{Type: "database", Driver: "sqlite", DSNEnv: "WEAVSTER_DB_POOL_TEST", Table: "t", Columns: map[string]string{"b": "b", "a": "a"}}
	if _, err := newSink(d); err == nil {
		t.Error("newSink without a pool: want error")
	}
	if _, err := buildSink(d, gateway.DefaultTLSOptions(), newDBPool()); err == nil || !strings.Contains(err.Error(), "WEAVSTER_DB_POOL_TEST is not set") {
		t.Errorf("unset variable: %v", err)
	}
	t.Setenv("WEAVSTER_DB_POOL_TEST", ":memory:")
	if _, err := buildSink(d, gateway.DefaultTLSOptions(), newDBPool()); err != nil {
		t.Errorf("buildSink: %v", err)
	}
}
