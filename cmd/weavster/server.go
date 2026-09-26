package main

import (
	"bytes"
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
	"sync"
	"syscall"
	"time"

	"github.com/weavster-dev/weavster/internal/adapters"
	"github.com/weavster-dev/weavster/internal/audit"
	"github.com/weavster-dev/weavster/internal/auth"
	"github.com/weavster-dev/weavster/internal/compiler"
	"github.com/weavster-dev/weavster/internal/flowlife"
	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/observability"
	"github.com/weavster-dev/weavster/internal/pipeline"
	"github.com/weavster-dev/weavster/internal/serverconfig"
	"github.com/weavster-dev/weavster/internal/state"
	"github.com/weavster-dev/weavster/internal/topology"
)

// buildServer wires the ports/adapters selected by cfg into the single binary
// (arch §3). One-time bootstrap output goes to out. The returned func
// releases the message store.
func buildServer(ctx context.Context, logger *slog.Logger, out io.Writer, cfg serverconfig.Config) (http.Handler, func() error, error) {
	h, closeFn, _, err := buildServerWithWorkers(ctx, logger, out, cfg)
	return h, closeFn, err
}

// buildServerWithWorkers is buildServer that also returns the background
// workers (delivery retries) for runServer to run for the server's lifetime.
func buildServerWithWorkers(ctx context.Context, logger *slog.Logger, out io.Writer, cfg serverconfig.Config) (http.Handler, func() error, func(context.Context), error) {
	store, err := openStore(ctx, logger, cfg)
	if err != nil {
		return nil, nil, nil, err
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
		return nil, nil, nil, err
	}
	if err := bootstrapAdminUser(ctx, provider, policy, out); err != nil {
		_ = closeStore()
		return nil, nil, nil, err
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
	stats, events := observability.NewStatsRegistry(), observability.NewEventLog()
	flows := flowAdapter{store: repo, stats: stats, locks: newFlowLocks()}
	if cfg.Flows.DeployOnStartup && store != nil {
		flows.DeployEnabled(ctx, logger)
	}
	var ingest gateway.MessageIngester
	workers := func(context.Context) {}
	if store != nil {
		pipe := pipeline.New(store, newSink, processingObserver{stats, events}, pipeline.Options{
			MaxAttempts: cfg.Delivery.MaxAttempts,
			BackoffBase: time.Duration(cfg.Delivery.BackoffBaseMs) * time.Millisecond,
			Gate:        flows.locks,
		})
		ia := ingestAdapter{flows: flows, pipe: pipe}
		ingest = ia
		workers = func(ctx context.Context) {
			retryLoop(ctx, ia, time.Duration(cfg.Delivery.RetryIntervalMs)*time.Millisecond, logger)
		}
	}

	srv := gateway.New(gateway.Config{
		Auth:        authAdapter{provider},
		Passwords:   passwordAdapter{provider},
		Authorizer:  authorizerAdapter{},
		Audit:       auditAdapter{sink},
		Flows:       flows,
		Messages:    messages,
		Ingest:      ingest,
		Lifecycle:   flows,
		FlowUpdates: flows,
		Stats:       statsAdapter{flows: flows, stats: stats},
		Events:      eventsAdapter{events},
		Topology:    topologyAdapter{flows: flows, stats: stats},
		System:      observability.SystemStatus("weavster", version, buildDate),
		RequireCSRF: cfg.Listen.RequireMarkerHeader,
	})
	return srv.Router(), closeStore, workers, nil
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
	handler, closeStore, workers, err := buildServerWithWorkers(ctx, logger, stderr, cfg)
	if err != nil {
		if ctx.Err() != nil {
			return 0
		}
		return fail(err)
	}
	defer func() { _ = closeStore() }()

	// Background workers run until runServer returns, and stop before the
	// store closes.
	workerCtx, stopWorkers := context.WithCancel(ctx)
	workersDone := make(chan struct{})
	go func() { workers(workerCtx); close(workersDone) }()
	defer stopWorkers()

	servers, err := listen(cfg, handler)
	if err != nil {
		stopWorkers()
		<-workersDone // no deliveries can be in progress this early
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
	stop() // a second SIGINT/SIGTERM now terminates immediately
	if !shutdown(servers, stopWorkers, workersDone, time.Duration(cfg.Listen.ShutdownTimeoutMs)*time.Millisecond, logger) {
		logger.Warn("shutdown deadline reached; unfinished messages are stored and resume on the next start",
			"timeout", time.Duration(cfg.Listen.ShutdownTimeoutMs)*time.Millisecond)
	}
	return code
}

// shutdown stops every listener at once and the retry worker (after its
// current message), then waits for in-flight requests and the worker until
// the deadline. It reports whether everything finished in time; on timeout
// the listeners are closed. Work still unfinished is already stored and is
// resumed by the next start's recovery pass with the same idempotency keys.
func shutdown(servers []*http.Server, stopWorkers context.CancelFunc, workersDone <-chan struct{}, timeout time.Duration, logger *slog.Logger) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	stopWorkers()
	var wg sync.WaitGroup
	clean := true
	var mu sync.Mutex
	for _, s := range servers {
		wg.Add(1)
		go func(s *http.Server) {
			defer wg.Done()
			if err := s.Shutdown(ctx); err != nil {
				_ = s.Close()
				mu.Lock()
				clean = false
				mu.Unlock()
			}
		}(s)
	}
	wg.Wait()
	select {
	case <-workersDone:
	case <-ctx.Done():
		logger.Warn("retry worker still delivering at the shutdown deadline; its message resumes on the next start")
		return false
	}
	return clean
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
	InsertUser(ctx context.Context, u state.UserDocument) error
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

func (a userStoreAdapter) InsertUser(ctx context.Context, u auth.User) error {
	doc, err := json.Marshal(u)
	if err != nil {
		return err
	}
	err = a.repo.InsertUser(ctx, state.UserDocument{Username: u.Username, Document: doc})
	if errors.Is(err, state.ErrUserExists) {
		return auth.ErrUserExists
	}
	return err
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
	case errors.Is(err, auth.ErrStorage), errors.Is(err, auth.ErrUserNotFound), errors.Is(err, auth.ErrPasswordConflict):
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
	UpdateFlow(ctx context.Context, f state.FlowDefinition) error
	GetFlow(ctx context.Context, id string) (state.FlowDefinition, error)
	ListFlows(ctx context.Context) ([]state.FlowDefinition, error)
	DeleteFlow(ctx context.Context, id string) error
}

// flowLocks serializes work per flow: message processing holds a flow's
// proc lock for reading; lifecycle changes and deletes hold it for writing,
// so they wait only for that flow's in-flight messages. change serializes
// status updates, including halt, which does not wait for processing.
type flowLocks struct {
	mu    sync.Mutex
	flows map[string]*flowLock
}

type flowLock struct {
	proc   sync.RWMutex
	change sync.Mutex
	refs   int // holders and waiters; the entry is removed at zero
}

func newFlowLocks() *flowLocks { return &flowLocks{flows: map[string]*flowLock{}} }

// acquire returns the flow's lock entry, counting the caller as a user.
// Entries are removed when unused, so unknown flow ids never accumulate.
func (l *flowLocks) acquire(id string) *flowLock {
	l.mu.Lock()
	defer l.mu.Unlock()
	fl, ok := l.flows[id]
	if !ok {
		fl = &flowLock{}
		l.flows[id] = fl
	}
	fl.refs++
	return fl
}

func (l *flowLocks) release(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if fl := l.flows[id]; fl != nil {
		if fl.refs--; fl.refs == 0 {
			delete(l.flows, id)
		}
	}
}

// ProcessFlow read-locks the flow for processing one message
// (pipeline.Options.Gate).
func (l *flowLocks) ProcessFlow(id string) func() {
	fl := l.acquire(id)
	fl.proc.RLock()
	return func() { fl.proc.RUnlock(); l.release(id) }
}

// exclusive locks the flow against status changes and, unless skipDrain,
// waits for its in-flight messages.
func (l *flowLocks) exclusive(id string, skipDrain bool) func() {
	fl := l.acquire(id)
	fl.change.Lock()
	if skipDrain {
		return func() { fl.change.Unlock(); l.release(id) }
	}
	fl.proc.Lock()
	return func() { fl.proc.Unlock(); fl.change.Unlock(); l.release(id) }
}

// flowAdapter stores gateway flows as JSON documents (D-12). stats, when
// set, is cleared for a flow when it is deleted.
type flowAdapter struct {
	store flowRepository
	stats *observability.StatsRegistry
	locks *flowLocks // nil: no coordination (tests)
}

func (a flowAdapter) lock(id string, skipDrain bool) func() {
	if a.locks == nil {
		return func() {}
	}
	return a.locks.exclusive(id, skipDrain)
}

// Transition applies a lifecycle action (spec §6.1). Every action except
// halt waits for the flow's in-flight messages; halt takes effect at once
// (force-stop, D-30).
func (a flowAdapter) Transition(ctx context.Context, id, action string) (gateway.Flow, error) {
	defer a.lock(id, action == flowlife.Halt)()
	f, err := a.Get(ctx, id)
	if err != nil {
		return gateway.Flow{}, err
	}
	next, err := flowlife.Next(f.Status, action)
	if errors.Is(err, flowlife.ErrUnknownAction) {
		return gateway.Flow{}, fmt.Errorf("%w: %q is not deploy, undeploy, start, stop, pause, halt, or resume", gateway.ErrUnknownAction, action)
	}
	if err != nil {
		return gateway.Flow{}, fmt.Errorf("%w: %w", gateway.ErrInvalidTransition, err)
	}
	return a.setStatus(ctx, f, next)
}

func (a flowAdapter) setStatus(ctx context.Context, f gateway.Flow, status string) (gateway.Flow, error) {
	f.Status = status
	doc, err := json.Marshal(f)
	if err != nil {
		return gateway.Flow{}, err
	}
	if err := a.store.UpdateFlow(ctx, state.FlowDefinition{ID: f.ID, Document: doc}); err != nil {
		return gateway.Flow{}, flowErr(err)
	}
	return f, nil
}

// Update replaces a flow's definition, keeping its runtime status (and
// enabled, when keepEnabled). It is serialized with lifecycle changes of the
// flow; later messages and queued retries use the new definition.
func (a flowAdapter) Update(ctx context.Context, id string, f gateway.Flow, keepEnabled bool) (gateway.Flow, error) {
	defer a.lock(id, true)()
	current, err := a.Get(ctx, id)
	if err != nil {
		return gateway.Flow{}, err
	}
	if keepEnabled {
		f.Enabled = current.Enabled
	}
	pf, err := toPipelineFlow(f)
	if err == nil {
		err = pipeline.Validate(pf)
	}
	if err != nil {
		return gateway.Flow{}, fmt.Errorf("%w: %w", gateway.ErrInvalidFlow, err)
	}
	f.ID = id
	return a.setStatus(ctx, f, current.Status)
}

// SetEnabled sets auto-deploy eligibility without changing the status.
func (a flowAdapter) SetEnabled(ctx context.Context, id string, enabled bool) (gateway.Flow, error) {
	defer a.lock(id, true)()
	f, err := a.Get(ctx, id)
	if err != nil {
		return gateway.Flow{}, err
	}
	f.Enabled = enabled
	return a.setStatus(ctx, f, f.Status)
}

// DeployEnabled deploys and starts every enabled flow that is undeployed
// (flows.deployOnStartup, spec §6.1). Each flow goes straight to started in
// one write, so a crash never leaves it half-deployed. Failures are logged
// and skipped: one bad flow never stops the server from starting.
func (a flowAdapter) DeployEnabled(ctx context.Context, logger *slog.Logger) {
	defs, err := a.store.ListFlows(ctx)
	if err != nil {
		logger.Warn("auto-deploy skipped: cannot list flows", "error", err)
		return
	}
	var started []string
	for _, d := range defs {
		var f gateway.Flow
		if err := json.Unmarshal(d.Document, &f); err != nil {
			logger.Warn("auto-deploy failed: unreadable flow", "flow", d.ID, "error", err)
			continue
		}
		if !f.Enabled || flowlife.Normalize(f.Status) != flowlife.Undeployed {
			continue
		}
		err := func() error {
			defer a.lock(f.ID, false)()
			current, err := a.Get(ctx, f.ID)
			if err != nil || !current.Enabled || current.Status != flowlife.Undeployed {
				return err
			}
			_, err = a.setStatus(ctx, current, flowlife.Started)
			return err
		}()
		if err != nil {
			logger.Warn("auto-deploy failed", "flow", f.ID, "error", err)
			continue
		}
		started = append(started, f.ID)
	}
	if len(started) > 0 {
		logger.Info("auto-deployed enabled flows", "flows", started)
	}
}

// RedeployAll undeploys and re-deploys every flow that is not undeployed;
// each ends deployed (D-29). Flows are handled one at a time, each after
// its in-flight messages finish, with a single status write. On error it
// returns the flows already redeployed.
func (a flowAdapter) RedeployAll(ctx context.Context) ([]gateway.Flow, error) {
	flows, err := a.List(ctx)
	if err != nil {
		return nil, err
	}
	out := []gateway.Flow{}
	for _, f := range flows {
		if f.Status == flowlife.Undeployed {
			continue
		}
		redeployed, ok, err := func() (gateway.Flow, bool, error) {
			defer a.lock(f.ID, false)()
			// Re-read under the lock: the flow may have been undeployed or
			// deleted since List.
			current, err := a.Get(ctx, f.ID)
			if errors.Is(err, gateway.ErrFlowNotFound) || (err == nil && current.Status == flowlife.Undeployed) {
				return gateway.Flow{}, false, nil
			}
			if err != nil {
				return gateway.Flow{}, false, err
			}
			updated, err := a.setStatus(ctx, current, flowlife.Deployed)
			return updated, err == nil, err
		}()
		if err != nil {
			return out, fmt.Errorf("redeploy %s: %w", f.ID, err)
		}
		if ok {
			out = append(out, redeployed)
		}
	}
	return out, nil
}

// toPipelineFlow converts a stored flow into the pipeline's definition,
// strictly decoding its transform.
func toPipelineFlow(f gateway.Flow) (pipeline.Flow, error) {
	pf := pipeline.Flow{ID: f.ID, Paused: !flowlife.AcceptsMessages(f.Status)}
	if raw := bytes.TrimSpace(f.Transform); len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		dec := json.NewDecoder(bytes.NewReader(f.Transform))
		dec.DisallowUnknownFields()
		var t compiler.Transform
		if err := dec.Decode(&t); err != nil {
			return pf, fmt.Errorf("transform: %w", err)
		}
		if t.Name == "" {
			t.Name = f.ID
		}
		if len(t.Steps) > 0 { // a transform without steps is a passthrough
			pf.Transform = &t
		}
	}
	for _, d := range f.Destinations {
		pf.Destinations = append(pf.Destinations, pipeline.Destination{Name: d.Name, Type: d.Type, URL: d.URL, Dir: d.Dir})
	}
	return pf, nil
}

// ingestAdapter runs received messages through their flow's pipeline.
type ingestAdapter struct {
	flows flowAdapter
	pipe  *pipeline.Pipeline
}

func (a ingestAdapter) Ingest(ctx context.Context, flowID string, body []byte) (gateway.IngestResult, error) {
	if a.flows.locks != nil {
		defer a.flows.locks.ProcessFlow(flowID)()
	}
	f, err := a.flows.Get(ctx, flowID)
	if err != nil {
		return gateway.IngestResult{}, err
	}
	pf, err := toPipelineFlow(f)
	if err == nil && pf.Paused {
		return gateway.IngestResult{}, fmt.Errorf("%w: flow %s is %s; start it first", gateway.ErrFlowNotRunning, f.ID, flowlife.Normalize(f.Status))
	}
	if err != nil {
		return gateway.IngestResult{}, err
	}
	// Processing is durable work: finish it even if the client disconnects,
	// so the stored message never stops half-way. HTTP deliveries are
	// bounded by adapters.HTTPSinkTimeout.
	res, err := a.pipe.Process(context.WithoutCancel(ctx), pf, body)
	if errors.Is(err, pipeline.ErrInvalidMessage) {
		return gateway.IngestResult{}, fmt.Errorf("%w: body must be a JSON object", gateway.ErrInvalidMessage)
	}
	if err != nil {
		return gateway.IngestResult{}, err
	}
	return gateway.IngestResult{ID: res.ID, Status: string(res.Status)}, nil
}

// retryLoop runs delivery retries immediately (resuming work queued before a
// restart) and then every interval until ctx is cancelled.
func retryLoop(ctx context.Context, ia ingestAdapter, interval time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := ia.retryDue(ctx); err != nil && ctx.Err() == nil {
			logger.Warn("delivery retry pass failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// retryDue runs one retry pass. The pipeline read-locks the flow gate per
// message, so flow deletes wait only for the message in progress.
func (a ingestAdapter) retryDue(ctx context.Context) error {
	_, err := a.pipe.RetryDue(ctx, func(ctx context.Context, id string) (pipeline.Flow, error) {
		f, err := a.flows.Get(ctx, id)
		if errors.Is(err, gateway.ErrFlowNotFound) {
			return pipeline.Flow{}, pipeline.ErrFlowGone
		}
		if err != nil {
			return pipeline.Flow{}, err
		}
		return toPipelineFlow(f)
	})
	return err
}

// adapterSink delivers through an adapters.Sink, passing the idempotency key
// as message metadata.
type adapterSink struct{ sink adapters.Sink }

func (s adapterSink) Write(ctx context.Context, d pipeline.Delivery) error {
	return s.sink.Write(ctx, adapters.Message{ID: d.MessageID, Body: d.Body, Metadata: map[string]string{
		adapters.IdempotencyKeyMetadata: d.IdempotencyKey,
		adapters.ContentTypeMetadata:    d.ContentType,
	}})
}

// newSink builds the adapter for a flow destination.
func newSink(d pipeline.Destination) (pipeline.Sink, error) {
	switch d.Type {
	case "http":
		return adapterSink{adapters.NewHTTPSink(d.URL)}, nil
	case "file":
		return adapterSink{adapters.NewFileSink(d.Dir)}, nil
	}
	return nil, fmt.Errorf("unsupported destination type %q", d.Type)
}

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
		f.Status = flowlife.Normalize(f.Status) // legacy statuses read as undeployed
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
	f.Status = flowlife.Normalize(f.Status) // legacy statuses read as undeployed
	return f, nil
}

func (a flowAdapter) Create(ctx context.Context, f gateway.Flow) (gateway.Flow, error) {
	pf, err := toPipelineFlow(f)
	if err == nil {
		err = pipeline.Validate(pf)
	}
	if err != nil {
		return gateway.Flow{}, fmt.Errorf("%w: %w", gateway.ErrInvalidFlow, err)
	}
	f.Status = flowlife.Undeployed // new flows are drafts (D-29)
	doc, err := json.Marshal(f)
	if err != nil {
		return gateway.Flow{}, err
	}
	if err := a.store.CreateFlow(ctx, state.FlowDefinition{ID: f.ID, Document: doc}); err != nil {
		return gateway.Flow{}, flowErr(err)
	}
	return f, nil
}

func (a flowAdapter) Delete(ctx context.Context, id string) error {
	defer a.lock(id, false)()
	if err := a.store.DeleteFlow(ctx, id); err != nil {
		return flowErr(err)
	}
	if a.stats != nil { // a new flow with this id starts from zero
		a.stats.Reset(id, false)
		a.stats.Reset(id, true)
	}
	return nil
}

// processingObserver turns finished messages into flow/destination counters
// and a message.<status> event.
type processingObserver struct {
	stats  *observability.StatsRegistry
	events *observability.EventLog
}

// Retried records a retry pass: per-destination outcomes for the attempted
// destinations, and the new status (sent or dead-lettered; still-queued
// messages are not counted again).
func (o processingObserver) Retried(m state.Message, attempted []string) {
	var kinds []observability.CounterKind
	switch m.Status {
	case state.StatusSent:
		kinds = []observability.CounterKind{observability.Sent}
	case state.StatusDeadLettered:
		kinds = []observability.CounterKind{observability.Errored}
	}
	connectors := map[string]observability.CounterKind{}
	for _, dest := range attempted {
		if m.Attempts[dest].LastError == "" {
			connectors[dest] = observability.Sent
		} else {
			connectors[dest] = observability.Errored
		}
	}
	o.stats.Record(m.FlowID, kinds, connectors)
	if m.Status != state.StatusQueued {
		o.events.Add("message."+string(m.Status), "", m.FlowID, map[string]string{"messageId": m.ID})
	}
}

// Received counts the message on arrival (and sets lastMessageAt).
func (o processingObserver) Received(flowID string) {
	o.stats.Inc(flowID, observability.Received)
}

// Processed records the outcome counters in one update and adds an event.
// The event carries only the message ID: error text can quote message
// content (PHI), and events are readable with events:view alone.
func (o processingObserver) Processed(m state.Message) {
	var kinds []observability.CounterKind
	switch m.Status {
	case state.StatusFiltered:
		kinds = []observability.CounterKind{observability.Filtered}
	case state.StatusErrored:
		kinds = []observability.CounterKind{observability.Errored}
	case state.StatusDeadLettered:
		kinds = []observability.CounterKind{observability.Transformed, observability.Errored}
	case state.StatusSent:
		kinds = []observability.CounterKind{observability.Transformed, observability.Sent}
	case state.StatusQueued:
		kinds = []observability.CounterKind{observability.Transformed, observability.Queued}
	}
	connectors := map[string]observability.CounterKind{}
	for dest, a := range m.Attempts {
		if a.LastError == "" {
			connectors[dest] = observability.Sent
		} else {
			connectors[dest] = observability.Errored
		}
	}
	o.stats.Record(m.FlowID, kinds, connectors)
	o.events.Add("message."+string(m.Status), "", m.FlowID, map[string]string{"messageId": m.ID})
}

type statsAdapter struct {
	flows gateway.FlowStore
	stats *observability.StatsRegistry
}

func (a statsAdapter) FlowStats(ctx context.Context, flowID string, lifetime bool) (gateway.FlowStats, error) {
	if _, err := a.flows.Get(ctx, flowID); err != nil {
		return gateway.FlowStats{}, err
	}
	s := a.stats.Snapshot(flowID, lifetime)
	out := gateway.FlowStats{
		Received: s.Received, Filtered: s.Filtered, Transformed: s.Transformed,
		Sent: s.Sent, Errored: s.Errored, Queued: s.Queued,
		Destinations: map[string]gateway.ConnectorStats{},
	}
	for name, c := range s.Connectors {
		out.Destinations[name] = gateway.ConnectorStats{Sent: c.Sent, Errored: c.Errored}
	}
	if s.LastMessageAt != nil {
		t := s.LastMessageAt.UTC()
		out.LastMessageAt = &t
	}
	return out, nil
}

type eventsAdapter struct{ log *observability.EventLog }

func (a eventsAdapter) SearchEvents(_ context.Context, q gateway.EventQuery) ([]gateway.Event, error) {
	found := a.log.Search(observability.EventFilter{Type: q.Type, Flow: q.FlowID, Limit: q.Limit})
	out := make([]gateway.Event, 0, len(found))
	for _, e := range found {
		out = append(out, gateway.Event{ID: e.ID, At: e.At.UTC(), Type: e.Type, FlowID: e.Flow, Data: e.Data})
	}
	return out, nil
}

type topologyAdapter struct {
	flows gateway.FlowStore
	stats *observability.StatsRegistry // nil: no activity
}

// activity is the flow's topology activity from its current counters.
func (t topologyAdapter) activity(flowID string) *topology.Activity {
	if t.stats == nil {
		return nil
	}
	s := t.stats.Snapshot(flowID, false)
	a := &topology.Activity{Received: s.Received, Sent: s.Sent, Errored: s.Errored, Queued: s.Queued}
	if s.LastMessageAt != nil {
		a.LastMessageAt = s.LastMessageAt.UTC().Format(time.RFC3339)
	}
	return a
}

func (t topologyAdapter) Overview(ctx context.Context) (topology.Graph, error) {
	flows, err := t.flows.List(ctx)
	if err != nil {
		return topology.Graph{}, err
	}
	summaries := make([]topology.FlowSummary, 0, len(flows))
	for _, f := range flows {
		summaries = append(summaries, topology.FlowSummary{ID: f.ID, Name: f.Name, Status: f.Status, Activity: t.activity(f.ID)})
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
