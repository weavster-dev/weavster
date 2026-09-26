package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/weavster-dev/weavster/internal/audit"
	"github.com/weavster-dev/weavster/internal/auth"
	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/observability"
	"github.com/weavster-dev/weavster/internal/serverconfig"
	"github.com/weavster-dev/weavster/internal/state"
	"github.com/weavster-dev/weavster/internal/topology"
)

// buildServer wires the ports/adapters selected by cfg into the single binary
// (arch §3). The returned func releases the message store.
func buildServer(ctx context.Context, logger *slog.Logger, cfg serverconfig.Config) (http.Handler, func() error, error) {
	store, err := openStore(ctx, logger, cfg.Store)
	if err != nil {
		return nil, nil, err
	}
	closeStore := func() error { return nil }
	var messages gateway.MessageSearcher
	if store != nil {
		closeStore = store.Close
		messages = &messageAdapter{store: store}
	}

	pp := cfg.Auth.PasswordPolicy
	provider := auth.NewLocalProvider(auth.Options{
		Policy: auth.PasswordPolicy{
			MinLength: pp.MinLength, MinUpper: pp.MinUpper, MinLower: pp.MinLower,
			MinNumeric: pp.MinNumeric, MinSpecial: pp.MinSpecial,
		},
		Lockout: auth.LockoutPolicy{
			RetryLimit: cfg.Auth.Lockout.RetryLimit, LockoutPeriod: cfg.Auth.Lockout.LockoutPeriodSeconds,
		},
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
		Messages:    messages,
		Topology:    &topologyAdapter{flows: flows},
		System:      observability.SystemStatus("weavster", version, buildDate),
		RequireCSRF: cfg.Listen.RequireMarkerHeader,
	})
	return srv.Router(), closeStore, nil
}

// openStore connects the configured message store, retrying failed attempts
// (spec §11). The disabled dialect returns a nil Store.
func openStore(ctx context.Context, logger *slog.Logger, sc serverconfig.Store) (state.Store, error) {
	var open func(context.Context, string) (state.Store, error)
	switch sc.Dialect {
	case serverconfig.DialectDisabled:
		logger.Warn("message store disabled; message endpoints return 503")
		return nil, nil
	case serverconfig.DialectMemory:
		return state.NewMemStore(), nil
	case serverconfig.DialectSQLite:
		if sc.DSN != ":memory:" && !strings.HasPrefix(sc.DSN, "file:") {
			if err := os.MkdirAll(filepath.Dir(sc.DSN), 0o700); err != nil {
				return nil, fmt.Errorf("store: %w", err)
			}
		}
		open = state.OpenSQLite
	default:
		open = func(ctx context.Context, dsn string) (state.Store, error) {
			return state.OpenPostgres(ctx, dsn, sc.MaxConnections)
		}
	}

	var err error
	for attempt := 1; attempt <= sc.MaxRetry+1; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(sc.RetryWaitMs) * time.Millisecond):
			}
		}
		var s state.Store
		if s, err = open(ctx, sc.DSN); err == nil {
			return s, nil
		}
		logger.Warn("store connection failed", "dialect", sc.Dialect, "attempt", attempt, "error", err)
	}
	return nil, fmt.Errorf("store: %s: giving up after %d attempts: %w", sc.Dialect, sc.MaxRetry+1, err)
}

// runServer loads the configuration, enforces the privileged-run guard
// (spec §11), and serves until SIGINT/SIGTERM.
func runServer(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "path to the server configuration file")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}

	cfg := serverconfig.Default()
	if *configPath != "" {
		var err error
		if cfg, err = serverconfig.Load(*configPath); err != nil {
			_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
	}
	if fs.NArg() > 0 {
		cfg.Listen.Address = fs.Arg(0)
	}
	if err := cfg.Validate(); err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	allowRoot := os.Getenv("WEAVSTER_ALLOW_ROOT") == "1"
	if err := checkPrivileged(allowRoot, isPrivileged); err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	// Register for signals before connecting or serving so a stop signal that
	// arrives as soon as the listener is up is never lost.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)

	logger := slog.New(slog.NewTextHandler(stderr, nil))
	handler, closeStore, err := buildServer(context.Background(), logger, cfg)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	defer func() { _ = closeStore() }()

	servers, err := listen(cfg, handler)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	errCh := make(chan error, len(servers))
	for _, s := range servers {
		go func(s *http.Server) {
			var err error
			if s.TLSConfig != nil {
				err = s.ListenAndServeTLS(cfg.TLS.CertFile, cfg.TLS.KeyFile)
			} else {
				err = s.ListenAndServe()
			}
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- err
			}
		}(s)
	}

	code := 0
	select {
	case err := <-errCh:
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		code = 1
	case <-sig:
	}
	for _, s := range servers {
		_ = s.Shutdown(context.Background())
	}
	return code
}

// listen builds the cleartext and TLS listeners enabled in cfg.
func listen(cfg serverconfig.Config, handler http.Handler) ([]*http.Server, error) {
	var servers []*http.Server
	if cfg.Listen.Address != "" {
		servers = append(servers, &http.Server{Addr: cfg.Listen.Address, Handler: handler, ReadHeaderTimeout: 10 * time.Second})
	}
	if cfg.Listen.TLSAddress != "" {
		opts := gateway.DefaultTLSOptions()
		if cfg.TLS.MinVersion == "1.3" {
			opts.MinVersion = tls.VersionTLS13
		}
		tlsCfg, err := gateway.BuildTLSConfig(opts)
		if err != nil {
			return nil, err
		}
		servers = append(servers, &http.Server{Addr: cfg.Listen.TLSAddress, Handler: handler, TLSConfig: tlsCfg, ReadHeaderTimeout: 10 * time.Second})
	}
	return servers, nil
}

// isPrivileged reports whether the process runs under a privileged OS account.
// It is a variable so acceptance tests can simulate a root account.
var isPrivileged = func() bool { return os.Geteuid() == 0 }

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
