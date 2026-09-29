package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/weavster-dev/weavster/internal/adapters"
	"github.com/weavster-dev/weavster/internal/audit"
	"github.com/weavster-dev/weavster/internal/auth"
	"github.com/weavster-dev/weavster/internal/compiler"
	"github.com/weavster-dev/weavster/internal/flowdef"
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
	secretsReader := newSecretReader(cfg.Secrets.Dir)
	store, err := openStore(ctx, logger, cfg, secretsReader)
	if err != nil {
		return nil, nil, nil, err
	}
	closeStore := func() error { return nil }
	var messages gateway.MessageStore
	var deadLetters gateway.DeadLetterRequeuer
	var trends gateway.MessageTrendReader
	if store != nil {
		closeStore = store.Close
	}

	pp := cfg.Auth.PasswordPolicy
	policy := auth.PasswordPolicy{
		MinLength: pp.MinLength, MinUpper: pp.MinUpper, MinLower: pp.MinLower,
		MinNumeric: pp.MinNumeric, MinSpecial: pp.MinSpecial,
	}
	// Only durable stores persist users; the memory dialect would just
	// duplicate the provider's own map.
	var users auth.UserStore
	if cfg.Store.Dialect == serverconfig.DialectPostgres {
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
	// Every state backend implements flowRepository, itemRepository, lookupRepository, and auditRepository
	// (TestStoresImplementFlowRepository).
	mem := state.NewMemStore()
	var repo flowRepository = mem
	var items itemRepository = mem
	var lookups lookupRepository = mem
	var audits auditRepository = mem
	if store != nil {
		repo = store.(flowRepository)
		items = store.(itemRepository)
		lookups = store.(lookupRepository)
		audits = store.(auditRepository)
	}
	stats, events := observability.NewStatsRegistry(), observability.NewEventLog()
	// Events kept across restarts (#107 D-94): the newest are loaded back
	// before anything records one (a flow deployed at start), and new ones
	// are stored in the background until the store closes.
	var ew *eventWriter
	if store != nil {
		ew = newEventWriter(store.(eventRepository), logger)
		if err := ew.restore(ctx, events, time.Now()); err != nil {
			_ = store.Close()
			return nil, nil, nil, fmt.Errorf("store: events: %w", err)
		}
		ew.start()
	}
	retention := time.Duration(cfg.Stats.RetentionHours) * time.Hour
	series := observability.NewTimeSeries(retention, maxStatsPoints)
	// Statistics kept across restarts (#107 D-97): loaded before a flow
	// deployed at start counts anything.
	var statsRepo statsRepository
	if cfg.Store.Dialect == serverconfig.DialectPostgres {
		statsRepo = store.(statsRepository)
		if err := restoreStats(ctx, statsRepo, stats, series, retention, time.Now()); err != nil {
			ew.stop()
			_ = store.Close()
			return nil, nil, nil, fmt.Errorf("store: statistics: %w", err)
		}
	}
	serverPorts := map[int]string{}
	for _, l := range listeners(cfg.Listen) {
		serverPorts[l.Port] = l.UsedBy
	}
	flows := flowAdapter{store: repo, stats: stats, series: series, locks: newFlowLocks(), defs: &sync.Mutex{}, events: events, serverPorts: serverPorts,
		statsSaves: &sync.Mutex{}}
	if cfg.Flows.DeployOnStartup && store != nil {
		flows.DeployEnabled(ctx, logger)
	}
	statsPort := statsAdapter{flows: flows, stats: stats, series: series, repo: statsRepo, retention: retention, logger: logger}
	var ingest gateway.MessageIngester
	var sourcePorts gateway.SourcePorts
	var prune gateway.Pruner
	var limit *processLimit
	retry := func(context.Context) {}
	if store != nil {
		sinks := &sinkFactory{logger: logger, tlsOpts: tlsOptions(cfg), dbs: newDBPool(secretsReader), delivered: func(ctx context.Context, flowID, key string) (string, error) {
			found, err := store.Search(ctx, state.Query{FlowID: flowID, Metadata: map[string]string{flowKeyMetadata: key}, Limit: 1})
			if err != nil || len(found) == 0 {
				return "", err
			}
			return found[0].ID, nil
		}}
		pipe := pipeline.New(store, sinks.build, processingObserver{stats, events}, pipeline.Options{
			MaxAttempts: cfg.Delivery.MaxAttempts,
			BackoffBase: time.Duration(cfg.Delivery.BackoffBaseMs) * time.Millisecond,
			Gate:        flows.locks,
		})
		limit = newProcessLimit(cfg.Processing, ctx.Done(), logger)
		ia := ingestAdapter{flows: flows, pipe: pipe, limit: limit}
		// Flow destinations hand messages to other flows (#107 D-70) inside
		// the sender's slot: a second slot could deadlock a full server.
		inProcess := ia
		inProcess.limit = nil
		sinks.ingest = inProcess
		ingest = ia
		ma := messageAdapter{store: store, pipe: pipe, ingest: ia}
		messages, deadLetters = ma, ma
		trends = messageAdapter{store: store}
		sources := newFileSources(flows, ia, eventLogRecorder{events}, logger)
		tables := newDatabaseSources(flows, ia, eventLogRecorder{events}, sinks.dbs, logger)
		listening := newPortSources(flows, ia, eventLogRecorder{events}, serverPorts, tlsOptions(cfg), cfg.TLS.KeyFile, secretsReader, logger)
		sourcePorts = listening
		pr := newPruner(cfg.Prune, ma, audits, store.(eventRepository), eventLogRecorder{events}, logger)
		prune = pr
		retry = func(ctx context.Context) {
			polled, served, queried, pruned := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
			go func() { pr.loop(ctx); close(pruned) }()        // message pruning (#107 D-88)
			go func() { sources.loop(ctx); close(polled) }()   // flows' file sources (#107 D-56)
			go func() { listening.loop(ctx); close(served) }() // flows' http and mllp sources (#107 D-57, D-60)
			go func() { tables.loop(ctx); close(queried) }()   // flows' database sources (#107 D-76)
			retryLoop(ctx, ia, time.Duration(cfg.Delivery.RetryIntervalMs)*time.Millisecond, logger)
			<-polled
			<-served
			<-queried
			<-pruned
		}
		closeStore = func() error { // after the API drained: nothing delivers any more
			saveCtx, cancel := context.WithTimeout(context.Background(), statsSaveLimit)
			if err := statsPort.saveNow(saveCtx, ""); err != nil {
				logger.Warn("statistics not stored at shutdown", "error", err)
			}
			cancel()
			ew.stop() // the last events, then no more
			sinks.dbs.close()
			return store.Close()
		}
	}

	workers := func(ctx context.Context) {
		sampled := make(chan struct{})
		go func() {
			statsPort.sampleLoop(ctx, time.Duration(cfg.Stats.SampleIntervalMs)*time.Millisecond, logger)
			close(sampled)
		}()
		retry(ctx)
		<-sampled
	}

	accounts := userAdminAdapter{p: provider, mu: &sync.Mutex{}}
	srv := gateway.New(gateway.Config{
		Auth:            authAdapter{provider},
		Passwords:       passwordAdapter{provider},
		Users:           accounts,
		Items:           itemsAdapter{repo: items},
		Snippets:        snippetsAdapter{repo: items, mu: &sync.Mutex{}},
		Alerts:          alertsAdapter{repo: items, mu: &sync.Mutex{}},
		ConfigValidator: configValidator{},
		Lookups:         lookupsAdapter{lookups},
		ConfigPlanner:   configPlanner{},
		Authorizer:      authorizerAdapter{},
		Audit:           auditAdapter{s: sink, repo: audits, logger: logger},
		AuditLog:        auditAdapter{s: sink, repo: audits, logger: logger, settle: auditSettle},
		Preferences:     accounts,
		PasswordCheck:   provider,
		Flows:           flows,
		Messages:        messages,
		Trends:          trends,
		Ingest:          ingest,
		Lifecycle:       flows,
		FlowUpdates:     flows,
		Transfer:        flows,
		Stats:           statsPort,
		DeadLetters:     deadLetters,
		Pruner:          prune,
		Metrics:         metricsHandler(serverMetrics{stats: stats, flows: flows, limit: limit, logger: logger}),
		ContextPath:     cfg.Listen.ContextPath,
		StatsHistory:    statsPort,
		Events:          eventsAdapter{events},
		Topology:        topologyAdapter{flows: flows, stats: stats},
		System:          newSystemAdapter(cfg, policy),
		Listeners:       listeners(cfg.Listen),
		Sources:         sourcePorts,
		RequireCSRF:     cfg.Listen.RequireMarkerHeader,
	})
	return srv.Router(), closeStore, workers, nil
}

// openStore connects the configured message store, retrying PostgreSQL
// connections (spec §11). The disabled dialect returns a nil Store.
func openStore(ctx context.Context, logger *slog.Logger, cfg serverconfig.Config, secrets secretReader) (state.Store, error) {
	sc := cfg.Store
	switch sc.Dialect {
	case serverconfig.DialectDisabled:
		logger.Warn("message store disabled; message endpoints return 503")
		return nil, nil
	case serverconfig.DialectMemory:
		return state.NewMemStore(), nil
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
		if s, err = openPostgres(ctx, sc, secrets); err == nil {
			return s, nil
		}
		if newer := (*state.NewerSchemaError)(nil); errors.As(err, &newer) {
			return nil, fmt.Errorf("store: %w", err) // a newer release's database: retrying cannot help
		}
		logger.Warn("store connection failed", "dialect", sc.Dialect, "attempt", attempt, "error", err)
	}
	return nil, fmt.Errorf("store: %s: giving up after %d attempts: %w", sc.Dialect, sc.MaxRetry+1, err)
}

// openPostgres connects to the store's PostgreSQL database, reading the
// connection string from the secret store.dsnEnv when that is set (at
// every attempt: the secret may appear meanwhile).
func openPostgres(ctx context.Context, sc serverconfig.Store, secrets secretReader) (state.Store, error) {
	dsn := sc.DSN
	if sc.DSNEnv != "" {
		var err error
		if dsn, err = secrets.value(ctx, sc.DSNEnv); err != nil {
			return nil, fmt.Errorf("store.dsnEnv: %w", err)
		}
	}
	return state.OpenPostgres(ctx, dsn, sc.MaxConnections)
}

// runServer enforces the privileged-run guard (spec §11), loads the
// configuration, and serves until SIGINT/SIGTERM. It exits 0 on help or a
// clean shutdown, 2 on a usage error, and 1 on any other failure (#107
// D-16, D-45). Usage is checked before the privileged-run guard.
func runServer(args []string, stderr io.Writer) int {
	failWith := func(code int, err error) int {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		return code
	}
	fail := func(err error) int { return failWith(1, err) }

	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "path to the server configuration file")
	if code, ok := parseFlags(fs, args, stderr); !ok {
		if code == 0 {
			fs.Usage()
		}
		return code
	}
	if fs.NArg() > 1 {
		return failWith(2, fmt.Errorf("unexpected arguments %q; put flags before the address", fs.Args()[1:]))
	}
	allowRoot := os.Getenv("WEAVSTER_ALLOW_ROOT") == "1"
	if err := checkPrivileged(allowRoot, isPrivileged); err != nil {
		return fail(err)
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

	// The API ports are bound before any background work starts: a port
	// that is taken stops the server before a source has taken a file or
	// a message.
	servers, err := listen(cfg, handler)
	if err != nil {
		return fail(err)
	}
	bound := make([]net.Listener, 0, len(servers))
	for _, s := range servers {
		ln, err := net.Listen("tcp", s.Addr)
		if err != nil {
			for _, b := range bound {
				_ = b.Close()
			}
			return fail(err)
		}
		bound = append(bound, ln)
	}

	// Background workers run until runServer returns, and stop before the
	// store closes.
	workerCtx, stopWorkers := context.WithCancel(ctx)
	workersDone := make(chan struct{})
	go func() { workers(workerCtx); close(workersDone) }()
	defer stopWorkers()

	errCh := make(chan error, len(servers))
	for i, s := range servers {
		go func(s *http.Server, ln net.Listener) {
			var err error
			if s.TLSConfig != nil {
				err = s.ServeTLS(ln, "", "")
			} else {
				err = s.Serve(ln)
			}
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- err
			}
		}(s, bound[i])
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

// listeners describes the configured listen addresses (ports-in-use).
func listeners(l serverconfig.Listen) []gateway.PortInUse {
	var out []gateway.PortInUse
	for _, e := range []struct{ addr, usedBy string }{{l.Address, "api"}, {l.TLSAddress, "api-tls"}} {
		if e.addr == "" {
			continue
		}
		// The config guarantees host:port with a resolvable, non-zero port.
		_, p, _ := net.SplitHostPort(e.addr)
		port, _ := net.LookupPort("tcp", p)
		out = append(out, gateway.PortInUse{Address: e.addr, Port: port, UsedBy: e.usedBy})
	}
	return out
}

// listen builds the cleartext and TLS listeners enabled in cfg.
func listen(cfg serverconfig.Config, handler http.Handler) ([]*http.Server, error) {
	var servers []*http.Server
	if cfg.Listen.Address != "" {
		servers = append(servers, &http.Server{Addr: cfg.Listen.Address, Handler: handler, ReadHeaderTimeout: 10 * time.Second})
	}
	if cfg.Listen.TLSAddress != "" {
		tlsCfg, err := loadTLS(tlsOptions(cfg), cfg.TLS.CertFile, cfg.TLS.KeyFile)
		if err != nil {
			return nil, err
		}
		servers = append(servers, &http.Server{Addr: cfg.Listen.TLSAddress, Handler: handler, TLSConfig: tlsCfg, ReadHeaderTimeout: 10 * time.Second})
	}
	return servers, nil
}

// loadTLS is the TLS configuration of an HTTPS listener (the API's or a
// flow source's) serving the certificate in certFile and keyFile.
func loadTLS(opts gateway.TLSOptions, certFile, keyFile string) (*tls.Config, error) {
	tlsCfg, err := gateway.BuildTLSConfig(opts)
	if err != nil {
		return nil, err
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("tls: %w", err)
	}
	tlsCfg.Certificates = []tls.Certificate{cert}
	return tlsCfg, nil
}

// tlsOptions are the HTTPS listener's settings: the listener and
// /api/v1/system both use them, so what is reported is what is served.
func tlsOptions(cfg serverconfig.Config) gateway.TLSOptions {
	opts := gateway.DefaultTLSOptions()
	if cfg.TLS.MinVersion == "1.3" {
		opts.MinVersion = tls.VersionTLS13
	}
	return opts
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

// auditRepository keeps audit entries (the store: PostgreSQL, SQLite, or
// memory).
type auditRepository interface {
	AppendAudit(ctx context.Context, r state.AuditRecord) (int64, error)
	SearchAudit(ctx context.Context, q state.AuditQuery) ([]state.AuditRecord, error)
	DeleteAuditBefore(ctx context.Context, t time.Time) (int, error)
}

// Stored audit entries (#107 D-93).
const (
	// auditWriteTimeout bounds the store write of one entry, so a slow
	// store cannot hold up the response it records.
	auditWriteTimeout = 2 * time.Second
	// auditSettle: a search returns entries at least this old. Ids are
	// taken when an entry is written but seen when it commits, which can
	// be out of order; by then every earlier entry has committed or
	// failed, so an afterId cursor never skips one.
	auditSettle = auditWriteTimeout + time.Second
)

// auditAdapter writes each audit entry to the store and to the log
// (stderr), redacted the same way and with the stored id, so both match. A
// store failure is logged and never changes the response (#107 D-93).
type auditAdapter struct {
	s      *audit.LocalSink
	repo   auditRepository
	logger *slog.Logger
	settle time.Duration // auditSettle; 0 in tests
	now    func() time.Time
}

func (a auditAdapter) Record(ctx context.Context, e gateway.AuditEvent) error {
	at := time.Now()
	detail := audit.RedactSensitive(e.Detail)
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), auditWriteTimeout)
	defer cancel()
	id, serr := a.repo.AppendAudit(sctx, state.AuditRecord{At: at, Actor: e.Actor, Action: e.Action, Resource: e.Resource, Detail: detail})
	if serr != nil {
		a.logger.Warn("audit entry not stored", "action", e.Action, "resource", e.Resource, "error", serr)
		id = 0 // the log line numbers it
	}
	return a.s.Record(ctx, audit.Entry{ID: id, At: at, Actor: e.Actor, Action: e.Action, Resource: e.Resource, Detail: detail})
}

// SearchAudit reads the stored audit entries.
func (a auditAdapter) SearchAudit(ctx context.Context, q gateway.AuditQuery) ([]gateway.AuditEntry, error) {
	sq := state.AuditQuery{Actor: q.Actor, Action: q.Action, Resource: q.Resource, From: q.From, To: q.To, AfterID: q.AfterID, Limit: q.Limit}
	if q.OmitReads {
		sq.ExcludeAction = gateway.AuditRead
	}
	if a.settle > 0 {
		now := time.Now
		if a.now != nil {
			now = a.now
		}
		if settled := now().Add(-a.settle); sq.To.IsZero() || sq.To.After(settled) {
			sq.To = settled
		}
	}
	records, err := a.repo.SearchAudit(ctx, sq)
	if err != nil {
		return nil, err
	}
	out := make([]gateway.AuditEntry, len(records))
	for i, r := range records {
		out[i] = gateway.AuditEntry{ID: r.ID, At: r.At, Actor: r.Actor, Action: r.Action, Resource: r.Resource, Detail: r.Detail}
	}
	return out, nil
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
	// series, when set, loses a deleted flow's statistics samples.
	series *observability.TimeSeries
	locks  *flowLocks // nil: no coordination (tests)
	// defs serializes definition changes (create, update, import, delete),
	// so dependency checks always see the flows they are written against.
	defs *sync.Mutex
	// events, when set, receives a flow.<status> event for every status
	// change and flow.deleted on removal.
	events *observability.EventLog
	// serverPorts are the server's own ports (port -> listener name), which
	// no flow source may use.
	serverPorts map[int]string
	// statsSaves orders statistics writes and flow deletes (which remove a
	// flow's stored statistics): a save takes it before it releases defs,
	// so a delete removes the stored statistics after any save of the
	// flow, never before.
	statsSaves *sync.Mutex
}

// definitions locks definition changes; it returns the unlock func.
func (a flowAdapter) definitions() func() {
	if a.defs == nil {
		return func() {}
	}
	a.defs.Lock()
	return a.defs.Unlock
}

// statsWrites takes the statistics-writes lock (flowAdapter.statsSaves).
func (a flowAdapter) statsWrites() func() {
	if a.statsSaves == nil {
		return func() {}
	}
	a.statsSaves.Lock()
	return a.statsSaves.Unlock
}

// definitionsWithin is definitions, giving up when ctx ends first.
func (a flowAdapter) definitionsWithin(ctx context.Context) (func(), error) {
	if a.defs == nil {
		return func() {}, nil
	}
	for !a.defs.TryLock() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	return a.defs.Unlock, nil
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
	return a.transition(ctx, id, action, nil, nil)
}

// errNotEligible is returned by transition when its check refuses the flow.
var errNotEligible = errors.New("flow not eligible")

// transition is Transition with an optional check of the flow, made under
// the flow's lock before anything changes (errNotEligible when it fails),
// and an optional changed callback, told (under the lock) of every flow
// whose status this call changed: dependencies deployed along the way, then
// the flow itself.
func (a flowAdapter) transition(ctx context.Context, id, action string, check func(gateway.Flow) bool, changed func(id string)) (gateway.Flow, error) {
	defer a.lock(id, action == flowlife.Halt)()
	f, err := a.Get(ctx, id)
	if err != nil {
		return gateway.Flow{}, err
	}
	if check != nil && !check(f) {
		return f, errNotEligible
	}
	next, err := flowlife.Next(f.Status, action)
	if errors.Is(err, flowlife.ErrUnknownAction) {
		return gateway.Flow{}, fmt.Errorf("%w: %q is not deploy, undeploy, start, stop, pause, halt, or resume", gateway.ErrUnknownAction, action)
	}
	if err != nil {
		return gateway.Flow{}, fmt.Errorf("%w: %w", gateway.ErrInvalidTransition, err)
	}
	if action == flowlife.Deploy {
		// Spec §6.1: deploy also deploys the flow's undeployed dependencies
		// (D-31). Locks are taken along dependency edges, which are acyclic,
		// so this cannot deadlock.
		if err := a.deployDependencies(ctx, f, changed); err != nil {
			return gateway.Flow{}, err
		}
	}
	out, err := a.setStatus(ctx, f, next)
	if err == nil && changed != nil {
		changed(id)
	}
	return out, err
}

// deployDependencies deploys every undeployed flow f depends on, directly or
// indirectly, dependencies first. Flows in any other state are left alone
// (D-31). It first collects the dependency closure, then locks it in one
// global (id-sorted) order, so concurrent deploys cannot deadlock. A missing
// dependency is ErrDependency; on a store failure the dependencies deployed
// so far stay deployed (visible via GET).
func (a flowAdapter) deployDependencies(ctx context.Context, root gateway.Flow, deployed func(id string)) error {
	closure := map[string]gateway.Flow{}
	var collect func(f gateway.Flow) error
	collect = func(f gateway.Flow) error {
		for _, depID := range f.Dependencies() {
			if _, seen := closure[depID]; seen || depID == root.ID {
				continue
			}
			dep, err := a.Get(ctx, depID)
			if errors.Is(err, gateway.ErrFlowNotFound) {
				return fmt.Errorf("%w: flow %s depends on missing flow %s", gateway.ErrDependency, f.ID, depID)
			}
			if err != nil {
				return fmt.Errorf("dependency %s: %w", depID, err)
			}
			closure[depID] = dep
			if err := collect(dep); err != nil {
				return err
			}
		}
		return nil
	}
	if err := collect(root); err != nil || len(closure) == 0 {
		return err
	}
	ids := make([]string, 0, len(closure))
	for id := range closure {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		defer a.lock(id, true)()
	}
	for _, id := range dependencyOrder(closure, ids) {
		dep, err := a.Get(ctx, id) // re-read under the lock
		if err != nil {
			return fmt.Errorf("dependency %s: %w", id, err)
		}
		if dep.Status == flowlife.Undeployed {
			if _, err := a.setStatus(ctx, dep, flowlife.Deployed); err != nil {
				return err
			}
			if deployed != nil {
				deployed(id)
			}
		}
	}
	return nil
}

// setStatus stores f with status, logging flow.<status> when the status
// changes.
func (a flowAdapter) setStatus(ctx context.Context, f gateway.Flow, status string) (gateway.Flow, error) {
	from := f.Status
	f.Status = status
	doc, err := json.Marshal(f)
	if err != nil {
		return gateway.Flow{}, err
	}
	if err := a.store.UpdateFlow(ctx, state.FlowDefinition{ID: f.ID, Document: doc}); err != nil {
		return gateway.Flow{}, flowErr(err)
	}
	if a.events != nil && from != status {
		a.events.Add("flow."+status, "", f.ID, map[string]string{"from": flowlife.Normalize(from)})
	}
	return f, nil
}

// SetDestinationRunning starts or stops one destination (spec §6.1
// per-connector start/stop), logging flow.destination.<started|stopped>.
func (a flowAdapter) SetDestinationRunning(ctx context.Context, id, destination string, running bool) (gateway.Flow, error) {
	// Stop waits for the flow's in-flight messages, so nothing reaches the
	// destination after the stop is confirmed; start needs no wait.
	defer a.lock(id, running)()
	f, err := a.Get(ctx, id)
	if err != nil {
		return gateway.Flow{}, err
	}
	if !hasDestination(f, destination) {
		return gateway.Flow{}, fmt.Errorf("%w: flow %s has no destination %q", gateway.ErrDestinationNotFound, id, destination)
	}
	stopped := slices.Contains(f.StoppedDestinations, destination)
	if stopped == !running {
		return f, nil // already in the requested state
	}
	if running {
		f.StoppedDestinations = slices.DeleteFunc(slices.Clone(f.StoppedDestinations), func(n string) bool { return n == destination })
	} else {
		f.StoppedDestinations = append(slices.Clone(f.StoppedDestinations), destination)
		sort.Strings(f.StoppedDestinations)
	}
	updated, err := a.setStatus(ctx, f, f.Status)
	if err == nil && a.events != nil {
		verb := "stopped"
		if running {
			verb = "started"
		}
		a.events.Add("flow.destination."+verb, "", id, map[string]string{"destination": destination})
	}
	return updated, err
}

func hasDestination(f gateway.Flow, name string) bool {
	return slices.ContainsFunc(f.Destinations, func(d gateway.FlowDestination) bool { return d.Name == name })
}

// Runtime state (status, stoppedDestinations) lives in the stored flow
// document but is not part of a definition. Every definition write goes
// through one of these two helpers.

// withRuntime returns def carrying current's runtime state; stopped
// destinations that def no longer has are dropped.
func withRuntime(current, def gateway.Flow) gateway.Flow {
	def.Status = current.Status
	def.StoppedDestinations = nil
	for _, name := range current.StoppedDestinations {
		if hasDestination(def, name) {
			def.StoppedDestinations = append(def.StoppedDestinations, name)
		}
	}
	return def
}

// withoutRuntime returns def with no runtime state, as for a new or
// exported definition; status is set to status.
func withoutRuntime(def gateway.Flow, status string) gateway.Flow {
	def.Status, def.StoppedDestinations = status, nil
	return def
}

// Update replaces a flow's definition, keeping its runtime status (and
// enabled, when keepEnabled). It is serialized with lifecycle changes of the
// flow; later messages and queued retries use the new definition.
func (a flowAdapter) Update(ctx context.Context, id string, f gateway.Flow, keepEnabled bool) (gateway.Flow, error) {
	defer a.definitions()()
	defer a.lock(id, true)()
	current, err := a.Get(ctx, id)
	if err != nil {
		return gateway.Flow{}, err
	}
	if keepEnabled {
		f.Enabled = current.Enabled
	}
	f.ID = id
	f = withRuntime(current, f)
	if err := a.validateDefinition(ctx, f); err != nil {
		return gateway.Flow{}, err
	}
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

// DeployEnabled deploys every enabled flow that is undeployed
// (flows.deployOnStartup, spec §6.1) into its initial state (started
// unless set). Each flow goes straight to that state in one write, so a crash never leaves it half-deployed. Failures are logged
// and skipped: one bad flow never stops the server from starting.
func (a flowAdapter) DeployEnabled(ctx context.Context, logger *slog.Logger) {
	defs, err := a.store.ListFlows(ctx)
	if err != nil {
		logger.Warn("auto-deploy skipped: cannot list flows", "error", err)
		return
	}
	// Snapshot the startup set first: a flow in it may be deployed as another
	// flow's dependency before its own turn, and must still be started.
	var pending []string
	inSet := map[string]bool{}
	for _, d := range defs {
		var f gateway.Flow
		if err := json.Unmarshal(d.Document, &f); err != nil {
			logger.Warn("auto-deploy failed: unreadable flow", "flow", d.ID, "error", err)
			continue
		}
		if f.Enabled && flowlife.Normalize(f.Status) == flowlife.Undeployed {
			pending = append(pending, f.ID)
			inSet[f.ID] = true
		}
	}
	deployed := map[string][]string{} // initial status -> flows
	for _, id := range pending {
		status, err := func() (string, error) {
			defer a.lock(id, false)()
			current, err := a.Get(ctx, id)
			if err != nil || !current.Enabled {
				return "", err
			}
			switch current.Status {
			case flowlife.Undeployed:
			case flowlife.Deployed: // deployed earlier in this pass as a dependency
			default:
				return "", nil
			}
			// Same as a manual deploy: undeployed dependencies are deployed.
			if err := a.deployDependencies(ctx, current, nil); err != nil {
				return "", err
			}
			status := initialStatus(current)
			_, err = a.setStatus(ctx, current, status)
			return status, err
		}()
		if err != nil {
			logger.Warn("auto-deploy failed", "flow", id, "error", err)
			continue
		}
		if status != "" {
			deployed[status] = append(deployed[status], id)
		}
	}
	for _, status := range []string{flowlife.Started, flowlife.Paused, flowlife.Stopped} {
		if len(deployed[status]) > 0 {
			logger.Info("auto-deployed enabled flows", "status", status, "flows", deployed[status])
		}
	}
}

// initialStatus is the status auto-deploy gives f: its initialState, or
// started.
func initialStatus(f gateway.Flow) string {
	if f.InitialState == "" {
		return flowlife.Started
	}
	return f.InitialState
}

// UpdateMany replaces several definitions (PUT /api/v1/flows). Every flow
// is checked, and must exist, before any is written; flows are written
// dependencies first, each keeping its status and stopped destinations.
// Like Update, it reads the other flows only when dependencies are set.
func (a flowAdapter) UpdateMany(ctx context.Context, changes []gateway.FlowChange) ([]string, error) {
	defer a.definitions()()
	updated := []string{}
	flows := make([]gateway.Flow, len(changes))
	for i, c := range changes {
		flows[i] = c.Flow
	}
	ids, err := checkBatch(flows)
	if err != nil {
		return updated, err
	}
	var missing []string
	withDeps := false
	for _, f := range flows {
		if _, err := a.Get(ctx, f.ID); errors.Is(err, gateway.ErrFlowNotFound) {
			missing = append(missing, f.ID)
		} else if err != nil {
			return updated, err
		}
		withDeps = withDeps || len(f.Dependencies()) > 0
	}
	if len(missing) > 0 {
		return updated, fmt.Errorf("%w: %s", gateway.ErrFlowNotFound, strings.Join(missing, ", "))
	}
	order := ids
	if withDeps {
		all, err := a.withFlows(ctx, flows...)
		if err != nil {
			return updated, err
		}
		if err := checkDependencies(all, ids); err != nil {
			return updated, fmt.Errorf("%w: %w", gateway.ErrInvalidFlow, err)
		}
		order = dependencyOrder(all, ids)
	}
	switch all, err := a.withFlows(ctx, flows...); {
	case err != nil && refersToFlows(flows...):
		return updated, err
	case err == nil:
		if err := checkSources(all, a.serverPorts); err != nil {
			return updated, fmt.Errorf("%w: %w", gateway.ErrInvalidFlow, err)
		}
	}
	keep := make(map[string]bool, len(changes))
	for _, c := range changes {
		keep[c.Flow.ID] = c.KeepEnabled
	}
	byID := make(map[string]gateway.Flow, len(flows))
	for _, f := range flows {
		byID[f.ID] = f
	}
	for _, id := range order {
		if err := a.replaceKeepingStatus(ctx, byID[id], keep[id]); err != nil {
			return updated, fmt.Errorf("%w: flow %s: %w", gateway.ErrUpdateIncomplete, id, err)
		}
		updated = append(updated, id)
	}
	return updated, nil
}

// checkBatch checks each definition of a multi-flow write (import, bulk
// update) and rejects repeated ids. It returns the ids in order.
func checkBatch(flows []gateway.Flow) ([]string, error) {
	seen := map[string]bool{}
	ids := make([]string, 0, len(flows))
	for _, f := range flows {
		if seen[f.ID] {
			return nil, fmt.Errorf("%w: flow %s appears twice", gateway.ErrInvalidFlow, f.ID)
		}
		seen[f.ID] = true
		ids = append(ids, f.ID)
		if err := checkDefinition(f); err != nil {
			return nil, fmt.Errorf("flow %s: %w", f.ID, err)
		}
	}
	return ids, nil
}

// TransitionAll applies action to every flow it applies to, in dependency
// order: dependencies first for deploy, start, and resume; dependents first
// otherwise. deploy skips disabled undeployed flows (checked under each
// flow's lock, as at startup), but an enabled flow's deploy still deploys its
// dependencies (D-31), disabled or not. Changed lists, in the order they
// happened, the flows this call changed (recorded under their locks);
// skipped lists every other flow with the reason. A store failure stops the
// run (ErrTransitionIncomplete): the failed flow is skipped as "failed" and
// the ones not reached as "not attempted".
func (a flowAdapter) TransitionAll(ctx context.Context, action string) (gateway.TransitionAllResult, error) {
	res := gateway.TransitionAllResult{Changed: []string{}, Skipped: []gateway.SkippedFlow{}}
	all, err := a.withFlows(ctx)
	if err != nil {
		return res, err
	}
	ids := make([]string, 0, len(all))
	for id := range all {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	order := dependencyOrder(all, ids)
	switch action {
	case flowlife.Deploy, flowlife.Start, flowlife.Resume:
	default:
		slices.Reverse(order)
	}
	var check func(gateway.Flow) bool
	if action == flowlife.Deploy {
		// Only undeployed flows would be deployed; others report why not.
		check = func(f gateway.Flow) bool { return f.Enabled || f.Status != flowlife.Undeployed }
	}
	done := map[string]bool{}
	record := func(id string) {
		done[id] = true
		res.Changed = append(res.Changed, id)
	}
	skipped := map[string]string{}
	var runErr error
	for i, id := range order {
		if done[id] { // already deployed as a dependency in this call
			continue
		}
		_, err := a.transition(ctx, id, action, check, record)
		switch {
		case err == nil:
		case errors.Is(err, errNotEligible):
			skipped[id] = "disabled"
		case errors.Is(err, gateway.ErrUnknownAction):
			return res, err
		case errors.Is(err, gateway.ErrInvalidTransition), errors.Is(err, gateway.ErrDependency), errors.Is(err, gateway.ErrFlowNotFound):
			skipped[id] = err.Error()
		default:
			runErr = fmt.Errorf("%w: flow %s: %w", gateway.ErrTransitionIncomplete, id, err)
			skipped[id] = "failed" // no internal detail in the reply
			for _, rest := range order[i+1:] {
				skipped[rest] = "not attempted"
			}
		}
		if runErr != nil {
			break
		}
	}
	for _, id := range order {
		if reason, ok := skipped[id]; ok && !done[id] {
			res.Skipped = append(res.Skipped, gateway.SkippedFlow{ID: id, Reason: reason})
		}
	}
	return res, runErr
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

// refersToFlows reports whether any of flows reads a source or depends on
// or sends to another flow, so its checks need every stored flow.
func refersToFlows(flows ...gateway.Flow) bool {
	for _, f := range flows {
		if f.Source != nil || len(f.Dependencies()) > 0 {
			return true
		}
	}
	return false
}

// checkSources refuses two flows reading the same directory, which would
// take the same files (#107 D-56) — or, with a recursive source, a
// directory or moveTo inside another flow's recursive directory (D-69) —
// and two flows listening on the same port or on one of the server's own
// ports (D-57).
func checkSources(all map[string]gateway.Flow, serverPorts map[int]string) error {
	ids := make([]string, 0, len(all))
	for id := range all {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	readBy, listenedBy := map[string]string{}, map[int]string{}
	for _, id := range ids {
		src := all[id].Source
		switch {
		case src == nil:
		case src.Listens():
			port, _ := flowdef.SourcePort(src) // checked with each definition
			if name := serverPorts[port]; name != "" {
				return fmt.Errorf("flow %s listens on port %d, the server's %s port", id, port, name)
			}
			if other, taken := listenedBy[port]; taken {
				return fmt.Errorf("flows %s and %s both listen on port %d; a port can have one flow source", other, id, port)
			}
			listenedBy[port] = id
		case src.Type == "file":
			dir := filepath.Clean(src.Dir)
			if other, taken := readBy[dir]; taken {
				return fmt.Errorf("flows %s and %s both read %s; a directory can have one file source", other, id, dir)
			}
			readBy[dir] = id
		}
	}
	if err := checkRecursiveSources(all, ids); err != nil {
		return err
	}
	return checkRoutes(all, ids)
}

// checkRoutes refuses a flow destination whose output its target flow
// cannot read (#107 D-70): it would fail on every message and be retried
// until dead-lettered.
func checkRoutes(all map[string]gateway.Flow, ids []string) error {
	for _, id := range ids {
		pf, err := toPipelineFlow(all[id])
		if err != nil {
			continue // reported by the flow's own checks
		}
		for _, d := range pf.Destinations {
			target, ok := all[d.Flow]
			if d.Type != "flow" || !ok {
				continue
			}
			sends, reads := pipeline.Receives(pf, d), readsFormat(target)
			if sends == "" && d.Transform == nil && pf.Transform == nil {
				sends = pf.InputFormat // passthrough keeps the input's declared format
			}
			if !formatFits(sends, reads) {
				return fmt.Errorf("flow %s: destination %s sends %s, which flow %s (inputFormat %s) cannot read", id, d.Name, sends, d.Flow, reads)
			}
		}
	}
	return nil
}

// readsFormat is the format a flow needs its messages in: its inputFormat
// (json when it transforms), or "" when it takes any bytes.
func readsFormat(f gateway.Flow) string {
	if f.InputFormat != "" && f.InputFormat != "json" {
		return f.InputFormat
	}
	transforms := len(f.Transform) > 0 && string(f.Transform) != "null"
	for _, d := range f.Destinations {
		transforms = transforms || (len(d.Transform) > 0 && string(d.Transform) != "null")
	}
	if transforms {
		return "json"
	}
	return ""
}

// formatFits reports whether output in format sends can be read as reads;
// unknown passthrough output ("") is allowed.
func formatFits(sends, reads string) bool {
	return reads == "" || sends == "" || sends == reads || (reads == "delimited" && sends == "text")
}

// checkRecursiveSources refuses a recursive file source whose directory
// holds another flow's source directory or moveTo: both flows would read
// the same files.
func checkRecursiveSources(all map[string]gateway.Flow, ids []string) error {
	for _, a := range ids {
		src := all[a].Source
		if src == nil || src.Type != "file" || !src.Recursive {
			continue
		}
		for _, b := range ids {
			other := all[b].Source
			if b == a || other == nil || other.Type != "file" {
				continue
			}
			for _, p := range []string{other.Dir, other.MoveTo} {
				if p != "" && flowdef.Within(p, src.Dir) {
					return fmt.Errorf("flow %s reads %s recursively, which holds flow %s's %s; a directory can have one file source", a, filepath.Clean(src.Dir), b, filepath.Clean(p))
				}
			}
		}
	}
	return nil
}

// checkDependencies validates dependsOn for the given root flows and
// everything they depend on: every dependency exists, no flow depends on
// itself, and there are no cycles. Flows outside that closure are ignored, so
// an unrelated bad edge never blocks a change.
func checkDependencies(all map[string]gateway.Flow, roots []string) error {
	const (
		unvisited = iota
		visiting
		done
	)
	mark := map[string]int{}
	var visit func(id string, path []string) error
	visit = func(id string, path []string) error {
		switch mark[id] {
		case visiting:
			return fmt.Errorf("dependency cycle: %s", strings.Join(append(path, id), " -> "))
		case done:
			return nil
		}
		mark[id] = visiting
		for _, dep := range all[id].Dependencies() {
			if dep == id {
				return fmt.Errorf("flow %s cannot depend on itself", id)
			}
			if _, ok := all[dep]; !ok {
				return fmt.Errorf("flow %s depends on unknown flow %s", id, dep)
			}
			if err := visit(dep, append(path, id)); err != nil {
				return err
			}
		}
		mark[id] = done
		return nil
	}
	sorted := append([]string(nil), roots...)
	sort.Strings(sorted)
	for _, id := range sorted {
		if err := visit(id, nil); err != nil {
			return err
		}
	}
	return nil
}

// dependencyOrder returns ids ordered so each flow's dependencies (within
// ids) come before it. The graph must be acyclic.
func dependencyOrder(all map[string]gateway.Flow, ids []string) []string {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	seen := map[string]bool{}
	var out []string
	var visit func(id string)
	visit = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		for _, dep := range all[id].Dependencies() {
			visit(dep)
		}
		if want[id] {
			out = append(out, id)
		}
	}
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	for _, id := range sorted {
		visit(id)
	}
	return out
}

// withFlows returns all stored flows by id, with the given flows replacing
// or adding to them.
func (a flowAdapter) withFlows(ctx context.Context, changed ...gateway.Flow) (map[string]gateway.Flow, error) {
	stored, err := a.List(ctx)
	if err != nil {
		return nil, err
	}
	all := make(map[string]gateway.Flow, len(stored)+len(changed))
	for _, f := range stored {
		all[f.ID] = f
	}
	for _, f := range changed {
		all[f.ID] = f
	}
	return all, nil
}

// checkDefinition validates a flow's transform and destinations.
func checkDefinition(f gateway.Flow) error {
	err := flowdef.CheckSource(f.Source)
	if err == nil {
		err = flowdef.CheckInput(f)
	}
	if err == nil {
		err = flowdef.CheckTransforms(f)
	}
	if err == nil {
		err = flowdef.CheckDestinations(f)
	}
	var pf pipeline.Flow
	if err == nil {
		pf, err = toPipelineFlow(f)
	}
	if err == nil {
		err = pipeline.Validate(pf)
	}
	if err != nil {
		return fmt.Errorf("%w: %w", gateway.ErrInvalidFlow, err)
	}
	return nil
}

// validateDefinition checks a flow's definition and its dependencies
// against the stored flows. Callers hold the definitions lock. Store errors
// are returned as-is (a 500), not as invalid input.
func (a flowAdapter) validateDefinition(ctx context.Context, f gateway.Flow) error {
	if err := checkDefinition(f); err != nil {
		return err
	}
	// Other flows can refer to f (a flow destination), so the checks
	// across flows run for every flow; an unreadable stored flow blocks
	// only a flow that itself refers to other flows.
	all, err := a.withFlows(ctx, f)
	if err != nil && refersToFlows(f) {
		return err
	} else if err != nil {
		return nil
	}
	if err := checkDependencies(all, []string{f.ID}); err != nil {
		return fmt.Errorf("%w: %w", gateway.ErrInvalidFlow, err)
	}
	if err := checkSources(all, a.serverPorts); err != nil {
		return fmt.Errorf("%w: %w", gateway.ErrInvalidFlow, err)
	}
	return nil
}

// Export returns the selected flows (all when ids is empty) and their
// transitive dependencies, without runtime status, sorted by id.
func (a flowAdapter) Export(ctx context.Context, ids []string) ([]gateway.Flow, error) {
	all, err := a.withFlows(ctx)
	if err != nil {
		return nil, err
	}
	selected := map[string]bool{}
	var add func(id string) error
	add = func(id string) error {
		if selected[id] {
			return nil
		}
		f, ok := all[id]
		if !ok {
			return fmt.Errorf("%w: %s", gateway.ErrFlowNotFound, id)
		}
		selected[id] = true
		for _, dep := range f.Dependencies() {
			if err := add(dep); err != nil {
				return err
			}
		}
		return nil
	}
	if len(ids) == 0 {
		for id := range all {
			selected[id] = true
		}
	}
	for _, id := range ids {
		if err := add(strings.TrimSpace(id)); err != nil {
			return nil, err
		}
	}
	out := make([]gateway.Flow, 0, len(selected))
	for id := range selected {
		f := all[id]
		out = append(out, withoutRuntime(f, "")) // runtime state is not exported
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Import validates the whole bundle, then writes it in dependency order:
// new flows are created undeployed; existing flows are replaced (keeping
// their current status) only with overwrite. It holds the definitions lock
// throughout. On a write failure it returns what was already written.
func (a flowAdapter) Import(ctx context.Context, flows []gateway.Flow, overwrite bool) (gateway.ImportResult, error) {
	defer a.definitions()()
	res := gateway.ImportResult{Created: []string{}, Updated: []string{}}
	ids, err := checkBatch(flows)
	if err != nil {
		return res, err
	}
	all, err := a.withFlows(ctx)
	if err != nil {
		return res, err
	}
	var conflicts []string
	existing := map[string]bool{}
	for _, f := range flows {
		if _, ok := all[f.ID]; ok {
			existing[f.ID] = true
			conflicts = append(conflicts, f.ID)
		}
	}
	byID := map[string]gateway.Flow{}
	for _, f := range flows {
		all[f.ID] = f
		byID[f.ID] = f
	}
	// Document errors (400) take precedence over conflicts (409).
	if err := checkDependencies(all, ids); err != nil {
		return res, fmt.Errorf("%w: %w", gateway.ErrInvalidFlow, err)
	}
	if err := checkSources(all, a.serverPorts); err != nil {
		return res, fmt.Errorf("%w: %w", gateway.ErrInvalidFlow, err)
	}
	if len(conflicts) > 0 && !overwrite {
		sort.Strings(conflicts)
		return res, fmt.Errorf("%w: %s (use overwrite=true to replace them)", gateway.ErrImportConflict, strings.Join(conflicts, ", "))
	}
	for _, id := range dependencyOrder(all, ids) {
		f := byID[id]
		if existing[id] {
			if err := a.replaceKeepingStatus(ctx, f, false); err != nil {
				return res, fmt.Errorf("%w: flow %s: %w", gateway.ErrImportIncomplete, id, err)
			}
			res.Updated = append(res.Updated, id)
			continue
		}
		f = withoutRuntime(f, flowlife.Undeployed)
		doc, err := json.Marshal(f)
		if err == nil {
			err = a.store.CreateFlow(ctx, state.FlowDefinition{ID: id, Document: doc})
		}
		if err != nil {
			return res, fmt.Errorf("%w: flow %s: %w", gateway.ErrImportIncomplete, id, err)
		}
		res.Created = append(res.Created, id)
	}
	return res, nil
}

// replaceKeepingStatus stores f's definition, re-reading the flow's current
// status (and, with keepEnabled, its enabled flag) under its lock so a
// concurrent lifecycle change or enable/disable is never undone.
func (a flowAdapter) replaceKeepingStatus(ctx context.Context, f gateway.Flow, keepEnabled bool) error {
	defer a.lock(f.ID, true)()
	current, err := a.Get(ctx, f.ID)
	if err != nil {
		return err
	}
	if keepEnabled {
		f.Enabled = current.Enabled
	}
	f = withRuntime(current, f)
	_, err = a.setStatus(ctx, f, f.Status)
	return err
}

// toPipelineFlow converts a stored flow into the pipeline's definition,
// strictly decoding its transform.
func toPipelineFlow(f gateway.Flow) (pipeline.Flow, error) {
	pf := pipeline.Flow{ID: f.ID, Paused: !flowlife.AcceptsMessages(f.Status), ResponseSelector: f.ResponseSelector, InputFormat: f.InputFormat}
	if d := f.Delimited; d != nil {
		if d.Delimiter != "" {
			pf.Delimiter = []rune(d.Delimiter)[0] // the schema allows one character
		}
		pf.NoHeader = d.Header != nil && !*d.Header
	}
	stopped := make(map[string]bool, len(f.StoppedDestinations))
	for _, name := range f.StoppedDestinations {
		stopped[name] = true
	}
	t, err := decodeTransform(f.Transform, f.ID)
	if err != nil {
		return pf, fmt.Errorf("transform: %w", err)
	}
	pf.Transform = t
	for _, d := range f.Destinations {
		t, err := decodeTransform(d.Transform, f.ID+"."+d.Name)
		if err != nil {
			return pf, fmt.Errorf("destination %s: transform: %w", d.Name, err)
		}
		rt, err := decodeTransform(d.ResponseTransform, f.ID+"."+d.Name+".response")
		if err != nil {
			return pf, fmt.Errorf("destination %s: responseTransform: %w", d.Name, err)
		}
		pf.Destinations = append(pf.Destinations, pipeline.Destination{
			Name: d.Name, Type: d.Type, URL: d.URL, Dir: d.Dir, Address: d.Address, TLS: d.TLS, CAFile: d.CAFile,
			FrameStart: d.FrameStart, FrameEnd: d.FrameEnd, AckMode: d.AckMode, Flow: d.Flow,
			Driver: d.Driver, DSNEnv: d.DSNEnv, Table: d.Table, Columns: d.Columns, KeyColumn: d.KeyColumn,
			Method: d.Method, Timeout: time.Duration(d.TimeoutMs) * time.Millisecond, MaxRedirects: d.MaxRedirects,
			Stopped: stopped[d.Name], Transform: t, ResponseTransform: rt,
		})
	}
	return pf, nil
}

// decodeTransform strictly decodes a stored DSL transform. It returns nil
// for an absent or null transform, or one without steps (a passthrough).
// name is used when the transform has none.
func decodeTransform(raw json.RawMessage, name string) (*compiler.Transform, error) {
	if raw = bytes.TrimSpace(raw); len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var t compiler.Transform
	if err := dec.Decode(&t); err != nil {
		return nil, err
	}
	if t.Name == "" {
		t.Name = name
	}
	if len(t.Steps) == 0 {
		return nil, nil
	}
	return &t, nil
}

// ingestAdapter runs received messages through their flow's pipeline.
type ingestAdapter struct {
	flows flowAdapter
	pipe  *pipeline.Pipeline
	// limit bounds the messages processed at once (nil: unbounded, for
	// in-process handoffs that already hold a slot).
	limit *processLimit
}

func (a ingestAdapter) Ingest(ctx context.Context, flowID string, body []byte) (gateway.IngestResult, error) {
	return a.ingest(ctx, flowID, body, nil)
}

// IngestFrom runs a message a flow's own source received (gateway.SourceIngester).
func (a ingestAdapter) IngestFrom(ctx context.Context, flowID string, body []byte, metadata map[string]string) (gateway.IngestResult, error) {
	return a.ingest(ctx, flowID, body, metadata)
}

// ingest runs body through flowID, storing metadata with the new message,
// within the processing limit when the adapter has one. The slot is taken
// after the flow's lock and its running check: a flow being stopped or
// changed makes its own messages wait, not fill every slot, and an unknown
// or stopped flow is answered at once.
func (a ingestAdapter) ingest(ctx context.Context, flowID string, body []byte, metadata map[string]string) (gateway.IngestResult, error) {
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
	if a.limit != nil {
		release, err := a.limit.acquire(ctx)
		if err != nil {
			return gateway.IngestResult{}, err
		}
		defer release()
	}
	// Processing is durable work: finish it even if the client disconnects,
	// so the stored message never stops half-way. HTTP deliveries are
	// bounded by each destination's timeoutMs (default 30 s).
	res, err := a.pipe.ProcessWithMetadata(context.WithoutCancel(ctx), pf, body, metadata)
	var invalid *pipeline.InvalidMessageError
	if errors.As(err, &invalid) {
		return gateway.IngestResult{}, fmt.Errorf("%w: %s", gateway.ErrInvalidMessage, invalid.Reason)
	}
	if err != nil { // ID is set when the message was stored before the error
		return gateway.IngestResult{ID: res.ID}, err
	}
	return gateway.IngestResult{ID: res.ID, Status: string(res.Status), Response: res.Response}, nil
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
	return adapters.Classify(s.sink.Write(ctx, adapterMessage(d))) // a code for the attempt record (#107 D-78)
}

// httpSink is the HTTP destination: a sink that also returns replies.
type httpSink struct{ sink *adapters.HTTPSink }

func (s httpSink) Write(ctx context.Context, d pipeline.Delivery) error {
	return adapters.Classify(s.sink.Write(ctx, adapterMessage(d)))
}

func (s httpSink) WriteResponse(ctx context.Context, d pipeline.Delivery) (*pipeline.Reply, error) {
	r, err := s.sink.WriteResponse(ctx, adapterMessage(d))
	if err != nil || r == nil {
		return nil, adapters.Classify(err)
	}
	return &pipeline.Reply{Body: r.Body, ContentType: r.ContentType}, nil
}

func adapterMessage(d pipeline.Delivery) adapters.Message {
	return adapters.Message{ID: d.MessageID, Body: d.Body, Metadata: map[string]string{
		adapters.IdempotencyKeyMetadata: d.IdempotencyKey,
		adapters.ContentTypeMetadata:    d.ContentType,
	}}
}

// sinkFactory builds destination sinks; flow destinations need the
// ingester, which exists only once the pipeline does, and a lookup of what
// they already delivered.
type sinkFactory struct {
	ingest gateway.SourceIngester
	// delivered finds the target's message stored for an idempotency key
	// ("" when there is none).
	delivered func(ctx context.Context, flowID, key string) (string, error)
	logger    *slog.Logger
	// tlsOpts are the server's TLS settings, also used by mllp
	// destinations with tls.
	tlsOpts gateway.TLSOptions
	// dbs are database destinations' connection pools.
	dbs *dbPool
}

func (s *sinkFactory) build(d pipeline.Destination) (pipeline.Sink, error) {
	if d.Type == "flow" {
		return flowSink{target: d.Flow, factory: s}, nil
	}
	return buildSink(d, s.tlsOpts, s.dbs)
}

// flowKeyMetadata stores a flow delivery's idempotency key with the target
// message, so a retry finds it instead of storing the message again.
const flowKeyMetadata = "source.idempotencyKey"

// flowSink hands a delivery to another flow as a new message of it (#107
// D-70); it is delivered once the target stored the message, and only
// once per delivery: a retry after a lost success finds the stored message.
type flowSink struct {
	target  string
	factory *sinkFactory
}

func (s flowSink) Write(ctx context.Context, d pipeline.Delivery) error {
	if id, err := s.factory.delivered(ctx, s.target, d.IdempotencyKey); err != nil {
		return fmt.Errorf("flow %s: %w", s.target, err)
	} else if id != "" {
		return nil // an earlier attempt stored it
	}
	res, err := s.factory.ingest.IngestFrom(ctx, s.target, d.Body, map[string]string{
		"source.flow": d.FlowID, "source.message": d.MessageID, flowKeyMetadata: d.IdempotencyKey,
	})
	switch {
	case err != nil && res.ID == "" && errors.Is(err, gateway.ErrFlowNotRunning):
		return adapters.WithCode("flow:not-running", fmt.Errorf("flow %s: %w", s.target, err))
	case err != nil && res.ID == "" && errors.Is(err, gateway.ErrFlowNotFound):
		return adapters.WithCode("flow:not-found", fmt.Errorf("flow %s: %w", s.target, err))
	case err != nil && res.ID == "":
		return fmt.Errorf("flow %s: %w", s.target, err)
	case err != nil: // stored: the target has it, and reports its own failure
		s.factory.logger.Warn("flow destination: the target stored the message but processing failed",
			"flow", d.FlowID, "message", d.MessageID, "target", s.target, "targetMessage", res.ID, "error", err)
	}
	return nil
}

// newSink builds the adapter for a flow destination.
func newSink(d pipeline.Destination) (pipeline.Sink, error) {
	return buildSink(d, gateway.DefaultTLSOptions(), nil)
}

// buildSink builds the adapter for a destination; an mllp destination
// with tls uses tlsOpts (the server's minimum version and ciphers), and a
// database destination a connection from dbs.
func buildSink(d pipeline.Destination, tlsOpts gateway.TLSOptions, dbs *dbPool) (pipeline.Sink, error) {
	switch d.Type {
	case "database":
		return databaseSink(d, dbs)
	case "http":
		return httpSink{adapters.NewHTTPSinkWith(d.URL, adapters.HTTPSinkOptions{
			Method: d.Method, Timeout: d.Timeout, MaxRedirects: d.MaxRedirects,
		})}, nil
	case "file":
		return adapterSink{adapters.NewFileSink(d.Dir)}, nil
	case "mllp":
		framing, err := mllpFraming(d.FrameStart, d.FrameEnd)
		if err != nil {
			return nil, err
		}
		var sink *adapters.MLLPSink
		if d.TLS {
			cfg, err := mllpClientTLS(d, tlsOpts)
			if err != nil {
				return nil, err
			}
			sink = adapters.NewMLLPSinkTLS(d.Address, d.Timeout, cfg)
		} else {
			sink = adapters.NewMLLPSinkWith(d.Address, d.Timeout)
		}
		return adapterSink{sink.WithMode(framing, d.AckMode == "none")}, nil
	}
	return nil, fmt.Errorf("unsupported destination type %q", d.Type)
}

// databaseSink builds a database destination's sink (#107 D-75): the
// connection string is read from the secret DSNEnv for each message, so a
// changed value applies without a restart.
func databaseSink(d pipeline.Destination, dbs *dbPool) (pipeline.Sink, error) {
	if dbs == nil {
		return nil, errors.New("database destinations need the server's connection pool")
	}
	db, err := dbs.open(context.Background(), d.Driver, d.DSNEnv)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(d.Columns))
	for n := range d.Columns {
		names = append(names, n)
	}
	sort.Strings(names)
	cols := make([]adapters.SQLColumn, len(names))
	for i, n := range names {
		cols[i] = adapters.SQLColumn{Name: n, Path: d.Columns[n]}
	}
	sink, err := adapters.NewSQLSink(db, adapters.SQLSinkOptions{Dialect: d.Driver, Table: d.Table, Columns: cols, KeyColumn: d.KeyColumn, Timeout: d.Timeout})
	if err != nil {
		return nil, err
	}
	return adapterSink{sink}, nil
}

// dbPool keeps one connection pool per driver and environment variable;
// when the variable's connection string changes (a rotated password), the
// old pool is closed and a new one opened.
type dbPool struct {
	mu     sync.Mutex
	dbs    map[string]pooledDB
	closed bool
	// secrets holds the connection strings (dsnEnv).
	secrets secretReader
}

// pooledDB is a pool and the connection string it was opened with.
type pooledDB struct {
	dsn string
	db  *sql.DB
}

func newDBPool(secrets secretReader) *dbPool {
	return &dbPool{dbs: map[string]pooledDB{}, secrets: secrets}
}

// open returns the pool for driver and the connection string in the secret
// dsnEnv.
func (p *dbPool) open(ctx context.Context, driver, dsnEnv string) (*sql.DB, error) {
	dsn, err := p.secrets.value(ctx, dsnEnv)
	if err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}
	return p.get(driver, dsnEnv, dsn)
}

// sqlDrivers are the database/sql drivers of the database destinations'
// drivers.
var sqlDrivers = map[string]string{adapters.DialectPostgres: "pgx", adapters.DialectSQLite: "sqlite"}

// get returns the pool for driver and the variable env holding dsn.
func (p *dbPool) get(driver, env, dsn string) (*sql.DB, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, errors.New("database: the server is stopping")
	}
	key := driver + "\x00" + env
	if old, ok := p.dbs[key]; ok {
		if old.dsn == dsn {
			return old.db, nil
		}
		_ = old.db.Close() // deliveries using it finish first (database/sql waits)
	}
	open := dsn
	if driver == adapters.DialectSQLite {
		open = sqliteWaits(dsn)
	}
	db, err := sql.Open(sqlDrivers[driver], open)
	if err != nil {
		return nil, errors.New("database: the connection string is not valid for the driver") // not the error: it can quote the string
	}
	if driver == adapters.DialectSQLite {
		db.SetMaxOpenConns(1) // SQLite has one writer: deliveries take turns instead of failing SQLITE_BUSY
	}
	p.dbs[key] = pooledDB{dsn: dsn, db: db}
	return db, nil
}

// sqliteBusyTimeout is how long a SQLite connection waits for another's
// lock (another program, or another flow's pool on the same file) before
// failing with "database is locked" (#389).
const sqliteBusyTimeout = "_pragma=busy_timeout(5000)"

// sqliteWaits is dsn with sqliteBusyTimeout, unless its query sets a busy
// timeout itself (a _pragma=busy_timeout parameter, not a path that happens
// to contain the words); it joins an existing query with &.
func sqliteWaits(dsn string) string {
	_, query, hasQuery := strings.Cut(dsn, "?")
	if !hasQuery {
		return dsn + "?" + sqliteBusyTimeout
	}
	for _, param := range strings.Split(query, "&") {
		if strings.HasPrefix(strings.ToLower(param), "_pragma=busy_timeout") {
			return dsn
		}
	}
	return dsn + "&" + sqliteBusyTimeout
}

// close closes every pool; later gets fail.
func (p *dbPool) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	for key, e := range p.dbs {
		_ = e.db.Close()
		delete(p.dbs, key)
	}
}

// mllpFraming is an mllp source's or destination's framing (#107 D-72).
func mllpFraming(start, end string) (adapters.MLLPFraming, error) {
	s, e, err := flowdef.MLLPFraming(start, end)
	return adapters.MLLPFraming{Start: s, End: e}, err
}

// mllpClientTLS verifies an mllp destination's receiver: its certificate
// against the system's roots, or only CAFile's when set, and its host name
// (the dialer takes it from the address), with the server's TLS settings
// (#107 D-71). CAFile is read for each message, so a replaced file takes
// effect without a restart.
func mllpClientTLS(d pipeline.Destination, opts gateway.TLSOptions) (*tls.Config, error) {
	cfg, err := gateway.BuildTLSConfig(opts)
	if err != nil {
		return nil, err
	}
	if d.CAFile == "" {
		return cfg, nil
	}
	pem, err := os.ReadFile(d.CAFile)
	if err != nil {
		return nil, fmt.Errorf("caFile: %w", err)
	}
	cfg.RootCAs = x509.NewCertPool()
	if !cfg.RootCAs.AppendCertsFromPEM(pem) {
		return nil, errors.New("caFile: no PEM certificate in the file")
	}
	return cfg, nil
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
	defer a.definitions()()
	if err := a.validateDefinition(ctx, f); err != nil {
		return gateway.Flow{}, err
	}
	f = withoutRuntime(f, flowlife.Undeployed) // new flows are drafts (D-29)
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
	defer a.definitions()()
	defer a.lock(id, false)() // waits for the flow's in-flight messages
	// Fail closed: every other flow must be readable to prove nothing
	// depends on this one. The flow being deleted may itself be unreadable,
	// so a corrupt flow can always be removed.
	defs, err := a.store.ListFlows(ctx)
	if err != nil {
		return err
	}
	var dependents []string
	var target *gateway.Flow
	for _, d := range defs {
		if d.ID == id {
			var f gateway.Flow
			if json.Unmarshal(d.Document, &f) == nil { // an unreadable flow is simply removed
				target = &f
			}
			continue
		}
		var f gateway.Flow
		if err := json.Unmarshal(d.Document, &f); err != nil {
			return fmt.Errorf("cannot check dependents: flow %s is unreadable: %w", d.ID, err)
		}
		if slices.Contains(f.Dependencies(), id) {
			dependents = append(dependents, f.ID)
		}
	}
	if len(dependents) > 0 {
		sort.Strings(dependents)
		return fmt.Errorf("%w: %s depended on by %s", gateway.ErrFlowInUse, id, strings.Join(dependents, ", "))
	}
	// A running flow is undeployed (persisted, flow.undeployed event) before
	// removal (spec §6.1).
	if target != nil && flowlife.Normalize(target.Status) != flowlife.Undeployed {
		if _, err := a.setStatus(ctx, *target, flowlife.Undeployed); err != nil {
			return err
		}
	}
	// The store removes the stored statistics with the flow, after any
	// save of them (see statsSaves).
	unlock := a.statsWrites()
	err = a.store.DeleteFlow(ctx, id)
	unlock()
	if err != nil {
		return flowErr(err)
	}
	if a.events != nil {
		a.events.Add("flow.deleted", "", id, nil)
	}
	if a.stats != nil { // a new flow with this id starts from zero
		a.stats.Reset(id, false)
		a.stats.Reset(id, true)
	}
	if a.series != nil {
		a.series.Forget(id)
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
	case state.StatusFiltered: // every destination's own filter dropped it
		kinds = []observability.CounterKind{observability.Filtered}
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
	flows  flowAdapter
	stats  *observability.StatsRegistry
	series *observability.TimeSeries
	// repo, when set, keeps the statistics and samples across restarts;
	// samples older than retention are dropped from it.
	repo      statsRepository
	retention time.Duration
	logger    *slog.Logger
}

func (a statsAdapter) FlowStats(ctx context.Context, flowID string, lifetime bool) (gateway.FlowStats, error) {
	if _, err := a.flows.Get(ctx, flowID); err != nil {
		return gateway.FlowStats{}, err
	}
	return toGatewayStats(a.stats.Snapshot(flowID, lifetime)), nil
}

// AllFlowStats returns the statistics of every flow.
func (a statsAdapter) AllFlowStats(ctx context.Context, lifetime bool) (map[string]gateway.FlowStats, error) {
	flows, err := a.flows.List(ctx)
	if err != nil {
		return nil, err
	}
	all := a.stats.SnapshotAll(lifetime) // one instant for every flow
	out := make(map[string]gateway.FlowStats, len(flows))
	for _, f := range flows {
		out[f.ID] = toGatewayStats(all[f.ID])
	}
	return out, nil
}

// ResetStats clears current statistics of one flow (all flows when flowID
// is empty), and lifetime totals too when lifetime is set.
func (a statsAdapter) ResetStats(ctx context.Context, flowID string, lifetime bool) error {
	if flowID != "" {
		if _, err := a.flows.Get(ctx, flowID); err != nil {
			return err
		}
	}
	a.stats.Clear(flowID, lifetime)
	// Stored now, so a restart does not bring the counts back. The reset
	// itself is done; a failed save is caught up by the next sample.
	if err := a.saveNow(ctx, flowID); err != nil {
		a.logger.Warn("statistics reset not stored yet", "flow", flowID, "error", err)
	}
	return nil
}

// eventLogRecorder records file-source events in the event log.
type eventLogRecorder struct{ log *observability.EventLog }

func (r eventLogRecorder) record(typ, flowID string, data map[string]string) {
	r.log.Add(typ, "", flowID, data)
}

// maxStatsPoints bounds the statistics samples kept for all flows together;
// past it the oldest are dropped.
const maxStatsPoints = 1000000

// sampleLoop records a sample now and every interval until ctx is
// cancelled (spec §2.11.37).
func (a statsAdapter) sampleLoop(ctx context.Context, interval time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := a.sample(ctx, time.Now()); err != nil && ctx.Err() == nil {
			logger.Warn("statistics sample failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// sample records every flow's lifetime statistics, taken at one instant.
// It holds the definitions lock, so a flow deleted meanwhile cannot have a
// sample recorded after Delete forgot its series.
func (a statsAdapter) sample(ctx context.Context, now time.Time) error {
	unlock := a.flows.definitions()
	flows, err := a.flows.List(ctx)
	if err != nil {
		unlock()
		return err
	}
	current, lifetime := a.stats.Snapshots()
	snap := make(map[string]observability.FlowStats, len(flows))
	ids := make([]string, len(flows))
	for i, f := range flows {
		snap[f.ID] = lifetime[f.ID]
		ids[i] = f.ID
	}
	a.series.RecordAll(now, snap)
	if a.repo == nil {
		unlock()
		return nil
	}
	defer a.flows.statsWrites()() // before unlock: see flowAdapter.statsSaves
	unlock()
	return a.save(ctx, ids, now.Round(0), current, lifetime, snap)
}

// StatsSeries returns the sampled statistics of one flow, or of every
// existing flow when q.FlowID is empty.
func (a statsAdapter) StatsSeries(ctx context.Context, q gateway.StatsSeriesQuery) ([]gateway.StatsSample, error) {
	keep := func(flow string) bool { return flow == q.FlowID }
	if q.FlowID != "" {
		if _, err := a.flows.Get(ctx, q.FlowID); err != nil {
			return nil, err
		}
	} else {
		flows, err := a.flows.List(ctx)
		if err != nil {
			return nil, err
		}
		exists := make(map[string]bool, len(flows))
		for _, f := range flows {
			exists[f.ID] = true
		}
		keep = func(flow string) bool { return exists[flow] }
	}
	points := a.series.Series(keep, q.From, q.To, q.Limit)
	out := make([]gateway.StatsSample, len(points))
	for i, p := range points {
		out[i] = gateway.StatsSample{At: p.At.UTC(), FlowID: p.Flow, Stats: toGatewayStats(p.Stats)}
	}
	return out, nil
}

func toGatewayStats(s observability.FlowStats) gateway.FlowStats {
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
	return out
}

type eventsAdapter struct{ log *observability.EventLog }

func eventFilter(q gateway.EventQuery) observability.EventFilter {
	return observability.EventFilter{Type: q.Type, Flow: q.FlowID, Since: q.From, Until: q.To, AfterID: q.AfterID, Cursor: q.Cursor, Limit: q.Limit}
}

func toGatewayEvent(e observability.Event) gateway.Event {
	return gateway.Event{ID: e.ID, At: e.At.UTC(), Type: e.Type, FlowID: e.Flow, Data: e.Data}
}

func (a eventsAdapter) SearchEvents(_ context.Context, q gateway.EventQuery) ([]gateway.Event, error) {
	found := a.log.Search(eventFilter(q))
	out := make([]gateway.Event, 0, len(found))
	for _, e := range found {
		out = append(out, toGatewayEvent(e))
	}
	return out, nil
}

func (a eventsAdapter) GetEvent(_ context.Context, id int64) (gateway.Event, error) {
	e, ok := a.log.Get(id)
	if !ok {
		return gateway.Event{}, gateway.ErrEventNotFound
	}
	return toGatewayEvent(e), nil
}

func (a eventsAdapter) CountEvents(_ context.Context, q gateway.EventQuery) (int, error) {
	return a.log.Count(eventFilter(q)), nil
}

func (a eventsAdapter) MaxEventID(context.Context) (int64, error) { return a.log.MaxID(), nil }

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
		var routes []string
		for _, d := range f.Destinations {
			if d.Type == "flow" && d.Flow != "" && !slices.Contains(routes, d.Flow) {
				routes = append(routes, d.Flow)
			}
		}
		summaries = append(summaries, topology.FlowSummary{ID: f.ID, Name: f.Name, Status: f.Status, Activity: t.activity(f.ID),
			Routes: routes, Deps: f.DependsOn})
	}
	return topology.Overview(summaries), nil
}

func (t topologyAdapter) FlowInternal(ctx context.Context, id string) (topology.Graph, error) {
	f, err := t.flows.Get(ctx, id)
	if err != nil {
		return topology.Graph{}, err
	}
	detail := topology.FlowDetail{ID: f.ID, Name: f.Name, Status: f.Status}
	if typ := f.SourceKind(); typ != "" {
		detail.Sources = []topology.Connector{{ID: f.ID + "-source", Label: typ + "://incoming", Type: typ, Status: f.Status}}
	}
	return topology.FlowInternal(detail), nil
}

// messageAdapter serves the message API from the store; deletes and
// reprocessing go through the pipeline, which knows what is in flight.
type messageAdapter struct {
	store  state.Store
	pipe   *pipeline.Pipeline
	ingest ingestAdapter
}

// toStateQuery maps an API search onto the store's query.
func toStateQuery(q gateway.MessageQuery) state.Query {
	sort := "-received_at"
	switch q.Sort {
	case "receivedAt":
		sort = "received_at"
	case "id", "-id":
		sort = q.Sort
	}
	return state.Query{
		FlowID: q.FlowID, Status: state.Status(q.Status), From: q.From, To: q.To,
		IDFrom: q.IDFrom, IDTo: q.IDTo, ContentType: q.ContentType,
		MinAttempts: q.MinAttempts, MaxAttempts: q.MaxAttempts, Metadata: q.Metadata,
		Limit: q.Limit, Offset: q.Offset, Sort: sort,
	}
}

// Export writes an archive of the messages matching q.
func (m messageAdapter) Export(ctx context.Context, q gateway.MessageQuery, key []byte) ([]byte, []string, error) {
	return state.ExportArchive(ctx, m.store, state.ExportOptions{Query: toStateQuery(q), Key: key})
}

// MessageTrends counts the store's messages per bucket and status.
func (m messageAdapter) MessageTrends(ctx context.Context, q gateway.MessageTrendQuery) (map[int]map[string]int, error) {
	counts, err := m.store.MessageTrends(ctx, state.TrendQuery{FlowID: q.FlowID, From: q.From, To: q.To, Bucket: q.Interval})
	return counts, err
}

// deletePage is how many matches DeleteMatching reads at a time (a
// variable so tests can page through a few messages).
var deletePage = 500

// DeleteMatching removes every message matching q's filters.
func (m messageAdapter) DeleteMatching(ctx context.Context, q gateway.MessageQuery) (deleted, busy int, err error) {
	q.Sort, q.Offset = "id", 0 // every filter, paged by id
	deleted, busyIDs, err := m.removeMatching(ctx, toStateQuery(q), nil)
	return deleted, len(busyIDs), err
}

// removeMatching removes every message matching sq that eligible accepts
// (every one when nil) and returns the ids skipped as busy. It pages by id
// from an exclusive cursor, so messages skipped do not shift the pages,
// and stops at an empty page or when ctx ends. Each message is checked
// again while the pipeline holds it, so one that changed since the search
// (a queued message delivered meanwhile) is kept.
func (m messageAdapter) removeMatching(ctx context.Context, sq state.Query, eligible func(state.Message) bool) (deleted int, busy []string, err error) {
	sq.Sort, sq.Offset, sq.Limit = "id", 0, deletePage
	accept := func(msg state.Message) bool { return sq.Matches(msg) && (eligible == nil || eligible(msg)) }
	for {
		if err := ctx.Err(); err != nil {
			return deleted, busy, err
		}
		page, err := m.store.Search(ctx, sq)
		if err != nil || len(page) == 0 {
			return deleted, busy, err
		}
		for _, msg := range page {
			if err := ctx.Err(); err != nil {
				return deleted, busy, err
			}
			if eligible != nil && !eligible(msg) {
				continue
			}
			release, ok := m.pipe.Hold(msg.ID)
			if !ok {
				busy = append(busy, msg.ID)
				continue
			}
			removed, err := m.removeIf(ctx, msg.ID, accept)
			release()
			if err != nil {
				return deleted, busy, err
			}
			if removed {
				deleted++
			}
		}
		sq.IDAfter = page[len(page)-1].ID
	}
}

// removeIfMatching deletes message id if it still matches sq's filters.
func (m messageAdapter) removeIfMatching(ctx context.Context, id string, sq state.Query) (bool, error) {
	return m.removeIf(ctx, id, sq.Matches)
}

// removeIf deletes message id if accept still accepts it as stored.
func (m messageAdapter) removeIf(ctx context.Context, id string, accept func(state.Message) bool) (bool, error) {
	msg, err := m.store.Get(ctx, id)
	if errors.Is(err, state.ErrNotFound) {
		return false, nil // removed meanwhile
	}
	if err != nil {
		return false, err
	}
	if !accept(msg) { // every filter, as the search applied them
		return false, nil
	}
	if err := m.store.Delete(ctx, id); err != nil && !errors.Is(err, state.ErrNotFound) {
		return false, err
	}
	return true, nil
}

// Import restores an archive. Every flow its messages will belong to must
// exist (checked before anything is written), and each message is written
// while the pipeline holds its id, so it never replaces one being processed.
func (m messageAdapter) Import(ctx context.Context, archive []byte, opts gateway.MessageImport) (gateway.MessageImportResult, error) {
	if opts.FlowID != "" { // checked even when the archive is empty
		if _, err := m.ingest.flows.Get(ctx, opts.FlowID); errors.Is(err, gateway.ErrFlowNotFound) {
			return gateway.MessageImportResult{}, fmt.Errorf("%w: flowId %s", gateway.ErrFlowNotFound, opts.FlowID)
		} else if err != nil {
			return gateway.MessageImportResult{}, err
		}
	}
	res, err := state.ImportArchive(ctx, m.store, archive, state.ImportOptions{
		FlowID: opts.FlowID, Overwrite: opts.Overwrite, Key: opts.Key,
		CheckFlow: func(flowID string) error {
			if _, err := m.ingest.flows.Get(ctx, flowID); err != nil {
				if errors.Is(err, gateway.ErrFlowNotFound) && opts.FlowID == "" {
					return fmt.Errorf("%w: the archive has messages of flow %s, which does not exist here; import with flowId", gateway.ErrFlowNotFound, flowID)
				}
				if errors.Is(err, gateway.ErrFlowNotFound) {
					return fmt.Errorf("%w: flowId %s", gateway.ErrFlowNotFound, flowID)
				}
				return err
			}
			return nil
		},
		Hold: m.pipe.Hold,
	})
	switch {
	case errors.Is(err, state.ErrBadArchive):
		err = fmt.Errorf("%w: %w", gateway.ErrInvalidArchive, err)
	case errors.Is(err, state.ErrImportIncomplete):
		err = fmt.Errorf("%w: %w", gateway.ErrMessageImportIncomplete, err)
	}
	return gateway.MessageImportResult{Imported: res.Imported, Skipped: res.Skipped, Busy: res.Busy}, err
}

func (m messageAdapter) Search(ctx context.Context, q gateway.MessageQuery) ([]gateway.Message, error) {
	msgs, err := m.store.Search(ctx, toStateQuery(q))
	if err != nil {
		return nil, err
	}
	out := make([]gateway.Message, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, toGatewayMessage(msg))
	}
	return out, nil
}

// Count is how many stored messages match q's filters.
func (m messageAdapter) Count(ctx context.Context, q gateway.MessageQuery) (int, error) {
	return m.store.Count(ctx, toStateQuery(q))
}

func (m messageAdapter) Get(ctx context.Context, id string) (gateway.Message, error) {
	msg, err := m.store.Get(ctx, id)
	if err != nil {
		return gateway.Message{}, messageErr(err)
	}
	return toGatewayMessage(msg), nil
}

func (m messageAdapter) Content(ctx context.Context, id, part string) (gateway.MessageContent, error) {
	msg, err := m.store.Get(ctx, id)
	if err != nil {
		return gateway.MessageContent{}, messageErr(err)
	}
	if part == "transformed" {
		if msg.Transformed == nil {
			return gateway.MessageContent{}, fmt.Errorf("%w: message %s has no transformed content (status %s)", gateway.ErrNoContent, id, msg.Status)
		}
		return gateway.MessageContent{Body: msg.Transformed, ContentType: pipeline.MimeType(msg.ContentType)}, nil
	}
	return gateway.MessageContent{Body: msg.Raw, ContentType: "application/octet-stream"}, nil
}

func (m messageAdapter) Delete(ctx context.Context, id string) error {
	err := m.pipe.Remove(ctx, id)
	if errors.Is(err, pipeline.ErrInFlight) {
		return gateway.ErrMessageBusy
	}
	return messageErr(err)
}

// Reprocess runs the message's original content through its flow again as
// a new message that keeps the original's metadata (except its processing
// error) and records where it came from.
func (m messageAdapter) Reprocess(ctx context.Context, id string) (gateway.IngestResult, error) {
	msg, err := m.store.Get(ctx, id)
	if err != nil {
		return gateway.IngestResult{}, messageErr(err)
	}
	body := msg.Original
	if body == nil {
		body = msg.Raw
	}
	md := make(map[string]string, len(msg.Metadata)+1)
	for k, v := range msg.Metadata {
		if k != "error" { // the new run records its own outcome
			md[k] = v
		}
	}
	md["reprocessedFrom"] = id
	return m.ingest.ingest(ctx, msg.FlowID, body, md)
}

// Requeue gives a dead-lettered message another round of delivery attempts
// through the pipeline (so never while it is in flight) and records how
// many attempts each destination had in a message.requeued event (never the
// error texts: they can quote message content). A message whose flow was
// deleted cannot be requeued.
func (m messageAdapter) Requeue(ctx context.Context, id string) (gateway.RequeueResult, error) {
	msg, err := m.store.Get(ctx, id)
	if err != nil {
		return gateway.RequeueResult{}, messageErr(err)
	}
	before, err := m.requeue(ctx, msg.ID, msg.FlowID, nil)
	if err != nil {
		return gateway.RequeueResult{}, err
	}
	after, err := m.store.Get(ctx, id)
	if err != nil {
		return gateway.RequeueResult{}, messageErr(err)
	}
	res := gateway.RequeueResult{Message: toGatewayMessage(after), Previous: toGatewayMessage(before).Attempts}
	if res.Previous == nil {
		res.Previous = map[string]gateway.MessageAttempt{}
	}
	return res, nil
}

// requeue requeues one message of flowID while holding the flow's gate, so
// a flow delete cannot slip between the check and the requeue. deleted
// remembers flows found deleted during a bulk requeue (nil: none); a flow
// that exists is checked again for every message, as it could be deleted
// between two of them. It returns the message as it was.
func (m messageAdapter) requeue(ctx context.Context, id, flowID string, deleted map[string]bool) (state.Message, error) {
	if gate := m.ingest.flows.locks; gate != nil {
		defer gate.ProcessFlow(flowID)()
	}
	gone := deleted[flowID]
	if !gone {
		_, err := m.ingest.flows.Get(ctx, flowID)
		if err != nil && !errors.Is(err, gateway.ErrFlowNotFound) {
			return state.Message{}, err
		}
		gone = err != nil
		if gone && deleted != nil {
			deleted[flowID] = true
		}
	}
	if gone {
		return state.Message{}, fmt.Errorf("%w: flow %s of message %s was deleted", gateway.ErrFlowNotFound, flowID, id)
	}
	before, err := m.pipe.Requeue(ctx, id)
	switch {
	case errors.Is(err, pipeline.ErrInFlight):
		return state.Message{}, gateway.ErrMessageBusy
	case errors.Is(err, pipeline.ErrNotDeadLettered): // keep the current status it reports
		return state.Message{}, fmt.Errorf("%w%s", gateway.ErrNotDeadLettered, strings.TrimPrefix(err.Error(), pipeline.ErrNotDeadLettered.Error()))
	case err != nil:
		return state.Message{}, messageErr(err)
	}
	if events := m.ingest.flows.events; events != nil {
		data := map[string]string{"messageId": id}
		for dest, a := range before.Attempts {
			data["previous."+dest+".attempts"] = strconv.Itoa(a.Attempts)
		}
		events.Add("message.requeued", "", flowID, data)
	}
	return before, nil
}

// RequeueAll requeues every dead-lettered message (of flowID when set),
// skipping those that are in flight or whose flow was deleted.
func (m messageAdapter) RequeueAll(ctx context.Context, flowID string) (gateway.RequeueAllResult, error) {
	if flowID != "" {
		if _, err := m.ingest.flows.Get(ctx, flowID); err != nil {
			return gateway.RequeueAllResult{}, err
		}
	}
	res := gateway.RequeueAllResult{Requeued: []string{}, Skipped: []gateway.RequeueSkip{}}
	deleted := map[string]bool{}
	cursor := ""
	for {
		page, err := m.store.Search(ctx, state.Query{Status: state.StatusDeadLettered, FlowID: flowID, IDAfter: cursor, Sort: "id", Limit: 500})
		if err != nil {
			return res, err
		}
		for _, msg := range page {
			_, err := m.requeue(ctx, msg.ID, msg.FlowID, deleted)
			switch {
			case err == nil:
				res.Requeued = append(res.Requeued, msg.ID)
			case errors.Is(err, gateway.ErrMessageBusy), errors.Is(err, gateway.ErrFlowNotFound),
				errors.Is(err, gateway.ErrNotDeadLettered), errors.Is(err, gateway.ErrMessageNotFound):
				res.Skipped = append(res.Skipped, gateway.RequeueSkip{ID: msg.ID, Reason: err.Error()})
			default:
				return res, err
			}
		}
		if len(page) < 500 {
			return res, nil
		}
		cursor = page[len(page)-1].ID
	}
}

// Remove deletes a message only if it is dead-lettered.
func (m messageAdapter) Remove(ctx context.Context, id string) error {
	err := m.pipe.RemoveDeadLettered(ctx, id)
	switch {
	case errors.Is(err, pipeline.ErrInFlight):
		return gateway.ErrMessageBusy
	case errors.Is(err, pipeline.ErrNotDeadLettered):
		return fmt.Errorf("%w%s", gateway.ErrNotDeadLettered, strings.TrimPrefix(err.Error(), pipeline.ErrNotDeadLettered.Error()))
	}
	return messageErr(err)
}

// messageErr translates the store's not-found error.
func messageErr(err error) error {
	if errors.Is(err, state.ErrNotFound) {
		return gateway.ErrMessageNotFound
	}
	return err
}

func toGatewayMessage(msg state.Message) gateway.Message {
	out := gateway.Message{
		ID: msg.ID, FlowID: msg.FlowID, Status: string(msg.Status), ContentType: msg.ContentType,
		ReceivedAt: msg.ReceivedAt.UTC(), UpdatedAt: msg.UpdatedAt.UTC(), Metadata: msg.Metadata,
	}
	if len(msg.Attempts) > 0 {
		out.Attempts = make(map[string]gateway.MessageAttempt, len(msg.Attempts))
		for dest, a := range msg.Attempts {
			ga := gateway.MessageAttempt{Attempts: a.Attempts, LastError: a.LastError, LastCode: a.LastCode}
			if !a.NextAttemptAt.IsZero() {
				t := a.NextAttemptAt.UTC()
				ga.NextAttemptAt = &t
			}
			if !a.LastAttemptAt.IsZero() {
				t := a.LastAttemptAt.UTC()
				ga.LastAttemptAt = &t
			}
			out.Attempts[dest] = ga
		}
	}
	return out
}
