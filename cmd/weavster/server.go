package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
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
// (arch §3). One-time bootstrap output goes to out. The returned func
// releases the message store.
func buildServer(ctx context.Context, logger *slog.Logger, out io.Writer, cfg serverconfig.Config) (http.Handler, func() error, error) {
	store, err := openStore(ctx, logger, cfg)
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
	policy := auth.PasswordPolicy{
		MinLength: pp.MinLength, MinUpper: pp.MinUpper, MinLower: pp.MinLower,
		MinNumeric: pp.MinNumeric, MinSpecial: pp.MinSpecial,
	}
	// Only durable stores persist users; the memory dialect would just
	// duplicate the provider's own map.
	var users auth.UserStore
	if cfg.Store.Dialect == serverconfig.DialectSQLite || cfg.Store.Dialect == serverconfig.DialectPostgres {
		// Every state backend implements userRepository
		// (TestStoresImplementUserRepository).
		users = userStoreAdapter{repo: store.(userRepository)}
	}
	provider := auth.NewLocalProvider(auth.Options{
		Store:  users,
		Logger: logger,
		Policy: policy,
		Lockout: auth.LockoutPolicy{
			RetryLimit: cfg.Auth.Lockout.RetryLimit, LockoutPeriod: cfg.Auth.Lockout.LockoutPeriodSeconds,
		},
		AntiEnumeration: true,
	})
	if err := provider.Load(ctx); err != nil {
		_ = closeStore()
		return nil, nil, err
	}
	if err := bootstrapAdminUser(ctx, provider, policy, out); err != nil {
		_ = closeStore()
		return nil, nil, err
	}

	sink := audit.NewLocalSink(logger)
	// Flow definitions live in the configured store; with the store
	// disabled they are kept in memory.
	// Every state backend implements flowRepository
	// (TestStoresImplementFlowRepository).
	var repo flowRepository = state.NewMemStore()
	if store != nil {
		repo = store.(flowRepository)
	}
	flows := flowAdapter{store: repo}

	srv := gateway.New(gateway.Config{
		Auth:        authAdapter{provider},
		Passwords:   passwordAdapter{provider},
		Authorizer:  authorizerAdapter{},
		Audit:       auditAdapter{sink},
		Flows:       flows,
		Messages:    messages,
		Topology:    topologyAdapter{flows: flows},
		System:      observability.SystemStatus("weavster", version, buildDate),
		RequireCSRF: cfg.Listen.RequireMarkerHeader,
	})
	return srv.Router(), closeStore, nil
}

// openStore connects the configured message store. Only PostgreSQL
// connections are retried (spec §11); SQLite failures are permanent. The
// disabled dialect returns a nil Store.
func openStore(ctx context.Context, logger *slog.Logger, cfg serverconfig.Config) (state.Store, error) {
	sc, dsn := cfg.Store, cfg.StoreDSN()
	switch sc.Dialect {
	case serverconfig.DialectDisabled:
		logger.Warn("message store disabled; message endpoints return 503")
		return nil, nil
	case serverconfig.DialectMemory:
		return state.NewMemStore(), nil
	case serverconfig.DialectSQLite:
		if dsn != ":memory:" && !strings.HasPrefix(dsn, "file:") {
			if err := os.MkdirAll(filepath.Dir(dsn), 0o700); err != nil {
				return nil, fmt.Errorf("store: %w", err)
			}
		}
		s, err := state.OpenSQLite(ctx, dsn)
		if err != nil {
			return nil, fmt.Errorf("store: sqlite: %w", err)
		}
		return s, nil
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
		if s, err = state.OpenPostgres(ctx, dsn, sc.MaxConnections); err == nil {
			return s, nil
		}
		logger.Warn("store connection failed", "dialect", sc.Dialect, "attempt", attempt, "error", err)
	}
	return nil, fmt.Errorf("store: %s: giving up after %d attempts: %w", sc.Dialect, sc.MaxRetry+1, err)
}

// runServer enforces the privileged-run guard (spec §11), loads the
// configuration, and serves until SIGINT/SIGTERM.
func runServer(args []string, stderr io.Writer) int {
	fail := func(err error) int {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	allowRoot := os.Getenv("WEAVSTER_ALLOW_ROOT") == "1"
	if err := checkPrivileged(allowRoot, isPrivileged); err != nil {
		return fail(err)
	}

	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "path to the server configuration file")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() > 1 {
		return fail(fmt.Errorf("unexpected arguments %q; put flags before the address", fs.Args()[1:]))
	}

	cfg := serverconfig.Default()
	if *configPath != "" {
		var err error
		if cfg, err = serverconfig.Load(*configPath); err != nil {
			return fail(err)
		}
	}
	if fs.NArg() == 1 {
		cfg.Listen.Address = fs.Arg(0)
	}
	if err := cfg.Validate(); err != nil {
		return fail(err)
	}

	// Cancel on SIGINT/SIGTERM from the start, so a stop signal interrupts
	// store connection retries and is never lost once listeners are up.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewTextHandler(stderr, nil))
	handler, closeStore, err := buildServer(ctx, logger, stderr, cfg)
	if err != nil {
		if ctx.Err() != nil {
			return 0
		}
		return fail(err)
	}
	defer func() { _ = closeStore() }()

	servers, err := listen(cfg, handler)
	if err != nil {
		return fail(err)
	}
	errCh := make(chan error, len(servers))
	for _, s := range servers {
		go func(s *http.Server) {
			var err error
			if s.TLSConfig != nil {
				err = s.ListenAndServeTLS("", "")
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
		code = fail(err)
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, s := range servers {
		_ = s.Shutdown(shutdownCtx)
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
		cert, err := tls.LoadX509KeyPair(cfg.TLS.CertFile, cfg.TLS.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("tls: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
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
	return gateway.Identity{Username: u.Username, Permissions: u.Permissions, MustChangePassword: u.MustChangePassword}, nil
}

// userRepository is the durable local-user store, implemented by state's
// SQL and in-memory stores.
type userRepository interface {
	PutUser(ctx context.Context, u state.UserDocument) error
	ListUsers(ctx context.Context) ([]state.UserDocument, error)
	DeleteUser(ctx context.Context, username string) error
}

// userStoreAdapter persists auth users as JSON documents.
type userStoreAdapter struct{ repo userRepository }

func (a userStoreAdapter) LoadUsers(ctx context.Context) ([]auth.User, error) {
	docs, err := a.repo.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]auth.User, 0, len(docs))
	for _, d := range docs {
		var u auth.User
		if err := json.Unmarshal(d.Document, &u); err != nil {
			return nil, fmt.Errorf("user %s: %w", d.Username, err)
		}
		out = append(out, u)
	}
	return out, nil
}

func (a userStoreAdapter) SaveUser(ctx context.Context, u auth.User) error {
	doc, err := json.Marshal(u)
	if err != nil {
		return err
	}
	return a.repo.PutUser(ctx, state.UserDocument{Username: u.Username, Document: doc})
}

func (a userStoreAdapter) DeleteUser(ctx context.Context, username string) error {
	return a.repo.DeleteUser(ctx, username)
}

type passwordAdapter struct{ p *auth.LocalProvider }

func (a passwordAdapter) ChangePassword(ctx context.Context, username, oldPassword, newPassword string) error {
	err := a.p.ChangePassword(ctx, username, oldPassword, newPassword)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, auth.ErrPasswordWrong):
		return gateway.ErrWrongPassword
	case errors.Is(err, auth.ErrStorage), errors.Is(err, auth.ErrUserNotFound):
		return err // internal failure
	default: // policy, reuse, or concurrent-change rejection
		return fmt.Errorf("%w: %w", gateway.ErrPasswordRejected, err)
	}
}

type authorizerAdapter struct{}

func (authorizerAdapter) Authorize(ctx context.Context, id gateway.Identity, resource, action string) bool {
	u := &auth.User{Username: id.Username, Permissions: id.Permissions}
	return auth.NewLocalAuthorizer().Authorize(ctx, u, resource, action)
}

type auditAdapter struct{ s *audit.LocalSink }

func (a auditAdapter) Record(ctx context.Context, e gateway.AuditEvent) error {
	return a.s.Record(ctx, audit.Entry{Actor: e.Actor, Action: e.Action, Resource: e.Resource, Detail: e.Detail})
}

// flowRepository is the durable flow-definition store (D-12), implemented by
// state's SQL and in-memory stores.
type flowRepository interface {
	CreateFlow(ctx context.Context, f state.FlowDefinition) error
	GetFlow(ctx context.Context, id string) (state.FlowDefinition, error)
	ListFlows(ctx context.Context) ([]state.FlowDefinition, error)
	DeleteFlow(ctx context.Context, id string) error
}

// flowAdapter stores gateway flows as JSON documents (D-12).
type flowAdapter struct{ store flowRepository }

// flowErr translates state's flow errors into the gateway's.
func flowErr(err error) error {
	switch {
	case errors.Is(err, state.ErrFlowNotFound):
		return gateway.ErrFlowNotFound
	case errors.Is(err, state.ErrFlowExists):
		return gateway.ErrFlowExists
	}
	return err
}

func (a flowAdapter) List(ctx context.Context) ([]gateway.Flow, error) {
	defs, err := a.store.ListFlows(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gateway.Flow, 0, len(defs))
	for _, d := range defs {
		var f gateway.Flow
		if err := json.Unmarshal(d.Document, &f); err != nil {
			return nil, fmt.Errorf("flow %s: %w", d.ID, err)
		}
		out = append(out, f)
	}
	return out, nil
}

func (a flowAdapter) Get(ctx context.Context, id string) (gateway.Flow, error) {
	d, err := a.store.GetFlow(ctx, id)
	if err != nil {
		return gateway.Flow{}, flowErr(err)
	}
	var f gateway.Flow
	if err := json.Unmarshal(d.Document, &f); err != nil {
		return gateway.Flow{}, fmt.Errorf("flow %s: %w", id, err)
	}
	return f, nil
}

func (a flowAdapter) Create(ctx context.Context, f gateway.Flow) error {
	doc, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return flowErr(a.store.CreateFlow(ctx, state.FlowDefinition{ID: f.ID, Document: doc}))
}

func (a flowAdapter) Delete(ctx context.Context, id string) error {
	return flowErr(a.store.DeleteFlow(ctx, id))
}

type topologyAdapter struct{ flows gateway.FlowStore }

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
