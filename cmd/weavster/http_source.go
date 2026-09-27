package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
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

	mu     sync.Mutex // guards open, which ports-in-use reads
	open   map[string]*sourceListener
	failed map[string]gateway.FlowSource // flow id -> source that could not listen (reported once)
}

func newHTTPSources(flows flowLister, ingest gateway.SourceIngester, events eventRecorder, reserved map[int]string, logger *slog.Logger) *httpSources {
	return &httpSources{flows: flows, ingest: ingest, events: events, reserved: reserved, logger: logger,
		open: map[string]*sourceListener{}, failed: map[string]gateway.FlowSource{}}
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
// source definition, and retried on the next reconcile.
func (s *httpSources) start(id string, src gateway.FlowSource) {
	port, _ := flowdef.SourcePort(&src) // validated with the definition
	var ln net.Listener
	err := fmt.Errorf("port %d is the server's %s port", port, s.reserved[port])
	if s.reserved[port] == "" {
		ln, err = net.Listen("tcp", src.Address)
	}
	if err != nil {
		if prev, seen := s.failed[id]; !seen || prev != src {
			s.failed[id] = src
			s.logger.Warn("http source: cannot listen", "flow", id, "address", src.Address, "error", err)
			s.events.record("source.http.failed", id, map[string]string{"address": src.Address})
		}
		return
	}
	delete(s.failed, id)
	l := &sourceListener{src: src, port: port, done: make(chan struct{})}
	handler := gateway.SourceHandler(id, src, s.ingest)
	l.srv = &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			l.handlers.Add(1)
			defer l.handlers.Done()
			handler.ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
	go func() { _ = l.srv.Serve(ln); close(l.done) }()
	s.mu.Lock()
	s.open[id] = l
	s.mu.Unlock()
	s.logger.Info("http source listening", "flow", id, "address", ln.Addr().String())
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
