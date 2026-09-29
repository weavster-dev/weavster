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

	"github.com/weavster-dev/weavster/internal/adapters"
	"github.com/weavster-dev/weavster/internal/flowdef"
	"github.com/weavster-dev/weavster/internal/flowlife"
	"github.com/weavster-dev/weavster/internal/gateway"
)

// sourceShutdown bounds how long a closing http source waits for idle
// connections before closing them; requests being processed still finish.
const sourceShutdown = 5 * time.Second

// sourceListener is one flow's open http or mllp source.
type sourceListener struct {
	src  gateway.FlowSource
	port int
	done <-chan struct{} // closed when the listener stops accepting
	// shut stops the listener and returns once the port is released and
	// no message is being processed (the store may close next).
	shut func()
}

// portSources opens the listener of every started flow that has an http
// or mllp source and closes it when the flow stops, changes, or goes away
// (#107 D-57, D-60). Flows are read once per flowRefresh, so a flow's port opens or
// closes within about a second of the change.
type portSources struct {
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
	// secrets holds the Basic passwords (passwordEnv).
	secrets secretValues

	mu     sync.Mutex // guards open, which ports-in-use reads
	open   map[string]*sourceListener
	failed map[string]sourceFailure // flow id -> last failure (each distinct one reported once)
}

// sourceFailure is why a source definition could not listen.
type sourceFailure struct {
	src    gateway.FlowSource
	reason string
}

func newPortSources(flows flowLister, ingest gateway.SourceIngester, events eventRecorder, reserved map[int]string, tlsOpts gateway.TLSOptions, serverKey string, secrets secretValues, logger *slog.Logger) *portSources {
	return &portSources{flows: flows, ingest: ingest, events: events, reserved: reserved, tlsOpts: tlsOpts, serverKey: serverKey, secrets: secrets, logger: logger,
		open: map[string]*sourceListener{}, failed: map[string]sourceFailure{}}
}

// loop keeps the listeners in step with the flows until ctx is cancelled,
// then closes them all.
func (s *portSources) loop(ctx context.Context) {
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
func (s *portSources) reconcile(ctx context.Context) {
	flows, err := s.flows.List(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.Warn("port sources: listing flows failed", "error", err)
		}
		return
	}
	want := map[string]gateway.FlowSource{}
	for _, f := range flows {
		if f.Source.Listens() && flowlife.Normalize(f.Status) == flowlife.Started {
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
			s.start(ctx, id, src)
		}
	}
}

// start opens id's listener; a failure is logged and recorded once per
// source definition and reason, and retried on the next reconcile.
func (s *portSources) start(ctx context.Context, id string, src gateway.FlowSource) {
	port, _ := flowdef.SourcePort(&src) // validated with the definition
	ln, password, tlsCfg, err := s.listen(ctx, src, port)
	if err != nil {
		if f := (sourceFailure{src, err.Error()}); s.failed[id] != f {
			s.failed[id] = f
			s.logger.Warn(src.Type+" source: cannot listen", "flow", id, "address", src.Address, "error", err)
			s.events.record("source."+src.Type+".failed", id, map[string]string{"address": src.Address, "reason": err.Error()})
		}
		return
	}
	delete(s.failed, id)
	l := &sourceListener{src: src, port: port}
	if src.Type == "mllp" {
		framing, _ := mllpFraming(src.FrameStart, src.FrameEnd) // validated with the definition
		if tlsCfg != nil {
			ln = tls.NewListener(ln, tlsCfg) // MLLP over TLS only (#107 D-71)
		}
		srv := adapters.ServeMLLP(ln, mllpHandler(id, s.ingest, src.AckMode != "none"), adapters.MLLPOptions{
			MaxFrame: gateway.MaxMessageBytes, IdleTimeout: mllpIdleTimeout, FrameTimeout: mllpFrameTimeout, HandshakeTimeout: mllpHandshakeTimeout,
			Framing: framing, NoReply: src.AckMode == "none",
		})
		l.done, l.shut = srv.Done(), func() { _ = srv.Close() }
	} else {
		l.done, l.shut = serveHTTPSource(id, src, ln, password, tlsCfg, s.ingest)
	}
	s.mu.Lock()
	s.open[id] = l
	s.mu.Unlock()
	s.logger.Info(src.Type+" source listening", "flow", id, "address", ln.Addr().String())
}

// serveHTTPSource serves an http source on ln. Its shut func stops
// accepting, closes connections still open after sourceShutdown, and waits
// until no request is being processed.
func serveHTTPSource(id string, src gateway.FlowSource, ln net.Listener, password string, tlsCfg *tls.Config, ingest gateway.SourceIngester) (<-chan struct{}, func()) {
	var handlers sync.WaitGroup // requests in progress
	handler := gateway.SourceHandler(id, src, password, ingest)
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlers.Add(1)
			defer handlers.Done()
			handler.ServeHTTP(w, r)
		}),
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: min(10*time.Second, readTimeout(src)),
		ReadTimeout:       readTimeout(src),
		IdleTimeout:       2 * time.Minute,
	}
	done := make(chan struct{})
	go func() {
		if tlsCfg != nil {
			_ = srv.ServeTLS(ln, "", "") // the certificate is in TLSConfig; also offers HTTP/2
		} else {
			_ = srv.Serve(ln)
		}
		close(done)
	}()
	return done, func() {
		ctx, cancel := context.WithTimeout(context.Background(), sourceShutdown)
		defer cancel()
		if srv.Shutdown(ctx) != nil {
			_ = srv.Close()
		}
		<-done
		handlers.Wait()
	}
}

// listen opens src's port, reads its Basic password from its secret,
// and loads its certificate (#107 D-58, D-71). A secured source whose password
// or certificate is missing stays closed rather than open without.
func (s *portSources) listen(ctx context.Context, src gateway.FlowSource, port int) (net.Listener, string, *tls.Config, error) {
	if name := s.reserved[port]; name != "" {
		return nil, "", nil, fmt.Errorf("port %d is the server's %s port", port, name)
	}
	var password string
	if src.PasswordEnv != "" {
		var err error
		if password, err = s.secrets.value(ctx, src.PasswordEnv); err != nil {
			return nil, "", nil, err
		}
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
func (s *portSources) isServerKey(keyFile string) bool {
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

// stopped reports whether the listener stopped accepting.
func (l *sourceListener) stopped() bool {
	select {
	case <-l.done:
		return true
	default:
		return false
	}
}

// close stops the listener; see sourceListener.shut.
func (l *sourceListener) close() { l.shut() }

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
func (s *portSources) closeAll() {
	s.mu.Lock()
	open := make([]*sourceListener, 0, len(s.open))
	for _, l := range s.open {
		open = append(open, l)
	}
	s.open = map[string]*sourceListener{}
	s.mu.Unlock()
	closeAll(open)
}

// Ports lists the open http and mllp sources for ports-in-use, by flow id.
func (s *portSources) Ports() []gateway.PortInUse {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]gateway.PortInUse, 0, len(s.open))
	for id, l := range s.open {
		out = append(out, gateway.PortInUse{Address: l.src.Address, Port: l.port, UsedBy: "flow:" + id})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UsedBy < out[j].UsedBy })
	return out
}
