package main

import (
	"context"
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

// sourceShutdown bounds how long a closing http source waits for the
// requests it is serving.
const sourceShutdown = 5 * time.Second

// sourceListener is one flow's open http source.
type sourceListener struct {
	src  gateway.FlowSource
	port int
	srv  *http.Server
	done chan struct{}
}

// httpSources opens the listener of every started flow that has an http
// source and closes it when the flow stops, changes, or goes away (#107
// D-57). Flows are read once per flowRefresh, so a flow's port opens or
// closes within about a second of the change.
type httpSources struct {
	flows interface {
		List(context.Context) ([]gateway.Flow, error)
	}
	ingest gateway.SourceIngester
	events eventRecorder
	logger *slog.Logger

	mu     sync.Mutex // guards open, which ports-in-use reads
	open   map[string]*sourceListener
	failed map[string]gateway.FlowSource // flow id -> source that could not listen (reported once)
}

func newHTTPSources(flows interface {
	List(context.Context) ([]gateway.Flow, error)
}, ingest gateway.SourceIngester, events eventRecorder, logger *slog.Logger) *httpSources {
	return &httpSources{flows: flows, ingest: ingest, events: events, logger: logger,
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
		if src, ok := want[id]; !ok || src != l.src {
			closing = append(closing, l)
			delete(s.open, id)
		}
	}
	s.mu.Unlock()
	for _, l := range closing { // closed before opening, so a moved port is free
		l.close()
	}
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
	ln, err := net.Listen("tcp", src.Address)
	if err != nil {
		if prev, seen := s.failed[id]; !seen || prev != src {
			s.failed[id] = src
			s.logger.Warn("http source: cannot listen", "flow", id, "address", src.Address, "error", err)
			s.events.record("source.http.failed", id, map[string]string{"address": src.Address})
		}
		return
	}
	delete(s.failed, id)
	l := &sourceListener{src: src, port: port, done: make(chan struct{}), srv: &http.Server{
		Handler:           gateway.SourceHandler(id, src, s.ingest),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       time.Minute,
		IdleTimeout:       2 * time.Minute,
	}}
	go func() { _ = l.srv.Serve(ln); close(l.done) }()
	s.mu.Lock()
	s.open[id] = l
	s.mu.Unlock()
	s.logger.Info("http source listening", "flow", id, "address", ln.Addr().String())
}

// close stops accepting, lets requests in progress finish (bounded), and
// waits for the listener to be released.
func (l *sourceListener) close() {
	ctx, cancel := context.WithTimeout(context.Background(), sourceShutdown)
	defer cancel()
	if l.srv.Shutdown(ctx) != nil {
		_ = l.srv.Close()
	}
	<-l.done
}

func (s *httpSources) closeAll() {
	s.mu.Lock()
	open := s.open
	s.open = map[string]*sourceListener{}
	s.mu.Unlock()
	for _, l := range open {
		l.close()
	}
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
