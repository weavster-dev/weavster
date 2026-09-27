package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/weavster-dev/weavster/internal/flowdef"
	"github.com/weavster-dev/weavster/internal/flowlife"
	"github.com/weavster-dev/weavster/internal/gateway"
)

// sourceShutdown bounds how long a closing http source waits for idle
// connections before closing them; requests being processed still finish.
const sourceShutdown = 5 * time.Second

// sourceListener is one flow's open http source.
type sourceListener struct {
	src  gateway.FlowSource
	port int
	srv  *http.Server
	done chan struct{} // closed when Serve returns
	// handlers counts requests in progress, so closing waits until no
	// message is being processed (the store may close next).
	handlers sync.WaitGroup
}

// httpSources opens the listener of every started flow that has an http
// source and closes it when the flow stops, changes, or goes away (#107
// D-57). Flows are read once per flowRefresh, so a flow's port opens or
// closes within about a second of the change.
type httpSources struct {
	flows  flowLister
	ingest gateway.SourceIngester
	events eventRecorder
	logger *slog.Logger
	// reserved are the server's own ports (port -> listener name), never
	// opened for a flow.
	reserved map[int]string
	// tlsOpts are the server's TLS settings, used by sources with a
	// certificate; serverKey is the API's own private key, which no flow
	// may serve.
	tlsOpts   gateway.TLSOptions
	serverKey string

	mu     sync.Mutex // guards open, which ports-in-use reads
	open   map[string]*sourceListener
	failed map[string]sourceFailure // flow id -> last failure (each distinct one reported once)
}

// sourceFailure is why a source definition could not listen.
type sourceFailure struct {
	src    gateway.FlowSource
	reason string
}

func newHTTPSources(flows flowLister, ingest gateway.SourceIngester, events eventRecorder, reserved map[int]string, tlsOpts gateway.TLSOptions, serverKey string, logger *slog.Logger) *httpSources {
	return &httpSources{flows: flows, ingest: ingest, events: events, reserved: reserved, tlsOpts: tlsOpts, serverKey: serverKey, logger: logger,
		open: map[string]*sourceListener{}, failed: map[string]sourceFailure{}}
}

// loop keeps the listeners in step with the flows until ctx is cancelled,
// then closes them all.
func (s *httpSources) loop(ctx context.Context) {
	ticker := time.NewTicker(flowRefresh)
	defer ticker.Stop()
	for {
		s.reconcile(ctx)
		select {
		case <-ctx.Done():
			s.closeAll()
			return
		case <-ticker.C:
		}
	}
}

// reconcile closes listeners no longer wanted and opens missing ones.
func (s *httpSources) reconcile(ctx context.Context) {
	flows, err := s.flows.List(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.Warn("http sources: listing flows failed", "error", err)
		}
		return
	}
	want := map[string]gateway.FlowSource{}
	for _, f := range flows {
		if f.Source != nil && f.Source.Type == "http" && flowlife.Normalize(f.Status) == flowlife.Started {
			want[f.ID] = *f.Source
		}
	}
	s.mu.Lock()
	var closing []*sourceListener
	for id, l := range s.open {
		// A listener whose Serve returned on its own is reopened.
		if src, ok := want[id]; !ok || src != l.src || l.stopped() {
			closing = append(closing, l)
			delete(s.open, id)
		}
	}
	s.mu.Unlock()
	closeAll(closing) // before opening, so a moved port is free
	for id := range s.failed {
		if _, ok := want[id]; !ok {
			delete(s.failed, id)
		}
	}
	for id, src := range want {
		s.mu.Lock()
		_, isOpen := s.open[id]
		s.mu.Unlock()
		if !isOpen && ctx.Err() == nil {
			s.start(id, src)
		}
	}
}

// start opens id's listener; a failure is logged and recorded once per
// source definition and reason, and retried on the next reconcile.
func (s *httpSources) start(id string, src gateway.FlowSource) {
	port, _ := flowdef.SourcePort(&src) // validated with the definition
	ln, password, tlsCfg, err := s.listen(src, port)
	if err != nil {
		if f := (sourceFailure{src, err.Error()}); s.failed[id] != f {
			s.failed[id] = f
			s.logger.Warn("http source: cannot listen", "flow", id, "address", src.Address, "error", err)
			s.events.record("source.http.failed", id, map[string]string{"address": src.Address, "reason": err.Error()})
		}
		return
	}
	delete(s.failed, id)
	l := &sourceListener{src: src, port: port, done: make(chan struct{})}
	handler := gateway.SourceHandler(id, src, password, s.ingest)
	l.srv = &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			l.handlers.Add(1)
			defer l.handlers.Done()
			handler.ServeHTTP(w, r)
		}),
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       readTimeout(src),
		IdleTimeout:       2 * time.Minute,
	}
	go func() {
		if tlsCfg != nil {
			_ = l.srv.ServeTLS(ln, "", "") // the certificate is in TLSConfig; also offers HTTP/2
		} else {
			_ = l.srv.Serve(ln)
		}
		close(l.done)
	}()
	s.mu.Lock()
	s.open[id] = l
	s.mu.Unlock()
	s.logger.Info("http source listening", "flow", id, "address", ln.Addr().String())
}

// listen opens src's port, reads its Basic password from the environment,
// and loads its certificate (#107 D-58). A secured source whose password
// or certificate is missing stays closed rather than open without.
func (s *httpSources) listen(src gateway.FlowSource, port int) (net.Listener, string, *tls.Config, error) {
	if name := s.reserved[port]; name != "" {
		return nil, "", nil, fmt.Errorf("port %d is the server's %s port", port, name)
	}
	password := os.Getenv(src.PasswordEnv)
	if src.PasswordEnv != "" && password == "" {
		return nil, "", nil, fmt.Errorf("environment variable %s is not set", src.PasswordEnv)
	}
	var tlsCfg *tls.Config
	if src.CertFile != "" {
		if s.isServerKey(src.KeyFile) {
			return nil, "", nil, errors.New("keyFile is the server's own TLS key; give the flow its own certificate")
		}
		var err error
		if tlsCfg, err = loadTLS(s.tlsOpts, src.CertFile, src.KeyFile); err != nil {
			return nil, "", nil, err
		}
	}
	ln, err := net.Listen("tcp", src.Address)
	if err != nil {
		return nil, "", nil, err
	}
	return ln, password, tlsCfg, nil
}

// isServerKey reports whether keyFile is the API's private key (also via
// another path or link).
func (s *httpSources) isServerKey(keyFile string) bool {
	if s.serverKey == "" {
		return false
	}
	a, errA := os.Stat(keyFile)
	b, errB := os.Stat(s.serverKey)
	return errA == nil && errB == nil && os.SameFile(a, b)
}

// readTimeout is the time allowed to read one request on src.
func readTimeout(src gateway.FlowSource) time.Duration {
	if src.ReadTimeoutMs > 0 {
		return time.Duration(src.ReadTimeoutMs) * time.Millisecond
	}
	return time.Minute
}

// stopped reports whether Serve has returned.
func (l *sourceListener) stopped() bool {
	select {
	case <-l.done:
		return true
	default:
		return false
	}
}

// close stops accepting, closes connections still open after
// sourceShutdown, and waits until the port is released and no request is
// being processed.
func (l *sourceListener) close() {
	ctx, cancel := context.WithTimeout(context.Background(), sourceShutdown)
	defer cancel()
	if l.srv.Shutdown(ctx) != nil {
		_ = l.srv.Close()
	}
	<-l.done
	l.handlers.Wait()
}

// closeAll closes listeners in parallel, so one slow request does not hold
// up the others.
func closeAll(ls []*sourceListener) {
	var wg sync.WaitGroup
	for _, l := range ls {
		wg.Add(1)
		go func() { defer wg.Done(); l.close() }()
	}
	wg.Wait()
}

// closeAll closes every open listener (server shutdown).
func (s *httpSources) closeAll() {
	s.mu.Lock()
	open := make([]*sourceListener, 0, len(s.open))
	for _, l := range s.open {
		open = append(open, l)
	}
	s.open = map[string]*sourceListener{}
	s.mu.Unlock()
	closeAll(open)
}

// Ports lists the open http sources for ports-in-use, by flow id.
func (s *httpSources) Ports() []gateway.PortInUse {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]gateway.PortInUse, 0, len(s.open))
	for id, l := range s.open {
		out = append(out, gateway.PortInUse{Address: l.src.Address, Port: l.port, UsedBy: "flow:" + id})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UsedBy < out[j].UsedBy })
	return out
}
