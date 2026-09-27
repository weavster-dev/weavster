package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/gateway"
)

// erringFlowList fails every List.
type erringFlowList struct{}

func (erringFlowList) List(context.Context) ([]gateway.Flow, error) {
	return nil, errors.New("store down")
}

func (erringFlowList) Get(context.Context, string) (gateway.Flow, error) {
	return gateway.Flow{}, errors.New("store down")
}

// sourceIngest is a gateway.SourceIngester that accepts everything.
type sourceIngest struct{}

func (sourceIngest) IngestFrom(context.Context, string, []byte, map[string]string) (gateway.IngestResult, error) {
	return gateway.IngestResult{ID: "m"}, nil
}

// TestHTTPSourcesReconcile: a changed source reopens its listener, as does
// one that stopped on its own; a failure (including a server port) is
// reported once and forgotten when the flow goes; a failed flow list
// changes nothing; ports-in-use is sorted by flow.
func TestPortSourcesReconcile(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	started := func(id, addr, path string) gateway.Flow {
		return gateway.Flow{ID: id, Status: "started", Source: &gateway.FlowSource{Type: "http", Address: addr, Path: path}}
	}
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()
	a, b := freeAddr(t), freeAddr(t)
	flows := &fakeFlowList{flows: []gateway.Flow{started("b", b, "/"), started("a", a, "/"),
		started("busy", held.Addr().String(), "/"), started("api", "127.0.0.1:8080", "/")}}
	events := &fakeEvents{}
	s := newPortSources(flows, sourceIngest{}, events, map[int]string{8080: "api"}, gateway.DefaultTLSOptions(), "", logger)
	defer s.closeAll()

	s.reconcile(ctx)
	s.reconcile(ctx)
	if ports := s.Ports(); len(ports) != 2 || ports[0].UsedBy != "flow:a" || ports[1].UsedBy != "flow:b" {
		t.Errorf("ports = %+v", ports)
	}
	if len(events.types) != 2 || events.types[0] != "source.http.failed" || events.types[1] != "source.http.failed" {
		t.Errorf("events = %v, want source.http.failed once for busy and once for api", events.types)
	}

	first := s.open["a"]
	flows.flows = []gateway.Flow{started("b", b, "/"), started("a", a, "/new")}
	s.reconcile(ctx)
	if s.open["a"] == first || s.open["a"].src.Path != "/new" {
		t.Error("a changed source kept its old listener")
	}
	if _, ok := s.failed["busy"]; ok {
		t.Error("the failure of a flow that went away was kept")
	}

	dead := s.open["b"]
	dead.shut()
	s.reconcile(ctx)
	if s.open["b"] == dead || s.open["b"].stopped() {
		t.Error("a listener that stopped on its own was not reopened")
	}

	s.flows = erringFlowList{}
	s.reconcile(ctx)
	if len(s.Ports()) != 2 {
		t.Error("a failed flow list closed the listeners")
	}
}

// reasonEvents records each event's reason.
type reasonEvents struct{ reasons []string }

func (e *reasonEvents) record(_, _ string, data map[string]string) {
	e.reasons = append(e.reasons, data["reason"])
}

// TestHTTPSourcesSecuredFailures: a certificate that cannot be loaded, or
// the server's own key, keeps the port closed with a reason; a new reason
// for the same definition is reported again.
func TestPortSourcesSecuredFailures(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dir := t.TempDir()
	certFile, keyFile, _ := selfSignedCert(t, dir)
	link := filepath.Join(dir, "link.key")
	if err := os.Symlink(keyFile, link); err != nil {
		t.Fatal(err)
	}
	src := func(id, addr, cert, key string) gateway.Flow {
		return gateway.Flow{ID: id, Status: "started", Source: &gateway.FlowSource{Type: "http", Address: addr, CertFile: cert, KeyFile: key}}
	}
	missing := filepath.Join(dir, "missing.pem")
	flows := &fakeFlowList{flows: []gateway.Flow{src("bad", freeAddr(t), missing, missing), src("server", freeAddr(t), certFile, link)}}
	events := &reasonEvents{}
	s := newPortSources(flows, sourceIngest{}, events, nil, gateway.DefaultTLSOptions(), keyFile, logger)
	defer s.closeAll()
	s.reconcile(ctx)
	s.reconcile(ctx)
	sort.Strings(events.reasons)
	if len(events.reasons) != 2 || !strings.Contains(events.reasons[0], "server's own TLS key") || !strings.HasPrefix(events.reasons[1], "tls: ") {
		t.Fatalf("reasons = %q", events.reasons)
	}
	if len(s.Ports()) != 0 {
		t.Error("a source without a usable certificate listens")
	}

	// Same definition, new reason: its certificate appears, but the port is
	// taken.
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()
	later := t.TempDir()
	own := src("own", held.Addr().String(), filepath.Join(later, "c.pem"), filepath.Join(later, "k.pem"))
	flows.flows = []gateway.Flow{own}
	events.reasons = nil
	s.reconcile(ctx)
	ownCert, ownKey, _ := selfSignedCert(t, t.TempDir())
	for from, to := range map[string]string{ownCert: own.Source.CertFile, ownKey: own.Source.KeyFile} {
		if err := os.Rename(from, to); err != nil {
			t.Fatal(err)
		}
	}
	s.reconcile(ctx)
	s.reconcile(ctx)
	if len(events.reasons) != 2 || !strings.HasPrefix(events.reasons[0], "tls: ") || !strings.Contains(events.reasons[1], "address already in use") {
		t.Errorf("reasons for one definition = %q", events.reasons)
	}
}
