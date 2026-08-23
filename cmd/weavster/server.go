package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/weavster-dev/weavster/internal/audit"
	"github.com/weavster-dev/weavster/internal/auth"
	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/observability"
	"github.com/weavster-dev/weavster/internal/state"
	"github.com/weavster-dev/weavster/internal/topology"
)

// buildServer wires all ports/adapters into the single binary (arch §3).
func buildServer(logger *slog.Logger) (http.Handler, error) {
	store := state.NewMemStore()

	provider := auth.NewLocalProvider(auth.Options{
		Policy:          auth.PasswordPolicy{MinLength: 8, MinUpper: 1, MinLower: 1, MinNumeric: 1},
		Lockout:         auth.LockoutPolicy{RetryLimit: 5, LockoutPeriod: 300},
		AntiEnumeration: true,
	})
	_ = provider.CreateUser(context.Background(), auth.User{
		Username: "admin", PasswordHash: "admin123!", Permissions: []string{auth.PermAdmin},
	})

	sink := audit.NewLocalSink(logger)
	flows := newMemFlowStore()

	srv := gateway.New(gateway.Config{
		Auth:        authAdapter{provider},
		Authorizer:  authorizerAdapter{},
		Audit:       auditAdapter{sink},
		Flows:       flows,
		Messages:    &messageAdapter{store: store},
		Topology:    &topologyAdapter{flows: flows},
		System:      observability.SystemStatus("weavster", version, buildDate),
		RequireCSRF: true,
	})
	return srv.Router(), nil
}

// runServer starts the composition root, enforcing the privileged-run guard
// (spec §11). Blocks until SIGINT/SIGTERM.
func runServer(args []string, stderr io.Writer) int {
	allowRoot := os.Getenv("WEAVSTER_ALLOW_ROOT") == "1"
	if err := checkPrivileged(allowRoot, isPrivileged); err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	addr := "127.0.0.1:8080"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		addr = args[0]
	}

	logger := slog.New(slog.NewTextHandler(stderr, nil))
	handler, err := buildServer(logger)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	server := &http.Server{Addr: addr, Handler: handler}

	errCh := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	case <-sig:
		_ = server.Shutdown(context.Background())
		return 0
	}
}

// isPrivileged reports whether the process runs under a privileged OS account.
func isPrivileged() bool { return os.Geteuid() == 0 }

// checkPrivileged refuses to run under a privileged account unless allowed
// (spec §11).
func checkPrivileged(allow bool, privileged func() bool) error {
	if allow {
		return nil
	}
	if privileged() {
		return errors.New("refusing to run under a privileged OS account; use a dedicated service account or set WEAVSTER_ALLOW_ROOT=1")
	}
	return nil
}

// --- adapters (composition-root glue) ---

type authAdapter struct{ p *auth.LocalProvider }

func (a authAdapter) Authenticate(ctx context.Context, username, password, mfaCode string) (gateway.Identity, error) {
	u, err := a.p.Authenticate(ctx, username, password, mfaCode)
	if err != nil {
		return gateway.Identity{}, err
	}
	return gateway.Identity{Username: u.Username, Permissions: u.Permissions}, nil
}

type authorizerAdapter struct{}

func (authorizerAdapter) Authorize(ctx context.Context, id gateway.Identity, resource, action string) bool {
	u := &auth.User{Username: id.Username, Permissions: id.Permissions}
	return auth.NewLocalAuthorizer().Authorize(ctx, u, resource, action)
}

type auditAdapter struct{ s *audit.LocalSink }

func (a auditAdapter) Record(ctx context.Context, actor, action, resource string) error {
	return a.s.Record(ctx, audit.Entry{Actor: actor, Action: action, Resource: resource})
}

type memFlowStore struct {
	mu    sync.Mutex
	flows map[string]gateway.Flow
}

func newMemFlowStore() *memFlowStore {
	return &memFlowStore{flows: map[string]gateway.Flow{
		"admit": {ID: "admit", Name: "Patient Admit", SourceType: "file", Status: "started", Enabled: true},
	}}
}

func (s *memFlowStore) List(context.Context) ([]gateway.Flow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]gateway.Flow, 0, len(s.flows))
	for _, f := range s.flows {
		out = append(out, f)
	}
	return out, nil
}

func (s *memFlowStore) Get(_ context.Context, id string) (gateway.Flow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.flows[id]
	if !ok {
		return gateway.Flow{}, fmt.Errorf("flow %s not found", id)
	}
	return f, nil
}

func (s *memFlowStore) Create(_ context.Context, f gateway.Flow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flows[f.ID] = f
	return nil
}

func (s *memFlowStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.flows, id)
	return nil
}

type topologyAdapter struct{ flows *memFlowStore }

func (t topologyAdapter) Overview(ctx context.Context) (topology.Graph, error) {
	flows, err := t.flows.List(ctx)
	if err != nil {
		return topology.Graph{}, err
	}
	summaries := make([]topology.FlowSummary, 0, len(flows))
	for _, f := range flows {
		summaries = append(summaries, topology.FlowSummary{ID: f.ID, Name: f.Name, Status: f.Status})
	}
	return topology.Overview(summaries), nil
}

func (t topologyAdapter) FlowInternal(ctx context.Context, id string) (topology.Graph, error) {
	f, err := t.flows.Get(ctx, id)
	if err != nil {
		return topology.Graph{}, err
	}
	detail := topology.FlowDetail{ID: f.ID, Name: f.Name, Status: f.Status}
	if f.SourceType != "" {
		detail.Sources = []topology.Connector{{ID: f.ID + "-source", Label: f.SourceType + "://incoming", Type: f.SourceType, Status: f.Status}}
	}
	return topology.FlowInternal(detail), nil
}

type messageAdapter struct{ store state.Store }

func (m messageAdapter) Search(ctx context.Context, q gateway.MessageQuery) ([]gateway.Message, error) {
	sq := state.Query{Limit: q.Limit}
	if q.Status != "" {
		sq.Status = state.Status(q.Status)
	}
	msgs, err := m.store.Search(ctx, sq)
	if err != nil {
		return nil, err
	}
	out := make([]gateway.Message, 0, len(msgs))
	for _, msg := range msgs {
		if q.FlowID != "" && msg.FlowID != q.FlowID {
			continue
		}
		out = append(out, gateway.Message{
			ID: msg.ID, FlowID: msg.FlowID, Status: string(msg.Status), ContentType: msg.ContentType,
		})
	}
	return out, nil
}
