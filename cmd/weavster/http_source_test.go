package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"

	"github.com/weavster-dev/weavster/internal/gateway"
)

// erringFlowList fails every List.
type erringFlowList struct{}

func (erringFlowList) List(context.Context) ([]gateway.Flow, error) {
	return nil, errors.New("store down")
}

// sourceIngest is a gateway.SourceIngester that accepts everything.
type sourceIngest struct{}

func (sourceIngest) IngestFrom(context.Context, string, []byte, map[string]string) (gateway.IngestResult, error) {
	return gateway.IngestResult{ID: "m"}, nil
}

// TestHTTPSourcesReconcile: a changed source reopens its listener, a
// failure is reported once and forgotten when the flow goes, a failed
// flow list changes nothing, and ports-in-use is sorted by flow.
func TestHTTPSourcesReconcile(t *testing.T) {
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
	flows := &fakeFlowList{flows: []gateway.Flow{started("b", b, "/"), started("a", a, "/"), started("busy", held.Addr().String(), "/")}}
	events := &fakeEvents{}
	s := newHTTPSources(flows, sourceIngest{}, events, logger)
	defer s.closeAll()

	s.reconcile(ctx)
	s.reconcile(ctx)
	if ports := s.Ports(); len(ports) != 2 || ports[0].UsedBy != "flow:a" || ports[1].UsedBy != "flow:b" {
		t.Errorf("ports = %+v", ports)
	}
	if len(events.types) != 1 || events.types[0] != "source.http.failed" {
		t.Errorf("events = %v, want one source.http.failed", events.types)
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

	s.flows = erringFlowList{}
	s.reconcile(ctx)
	if len(s.Ports()) != 2 {
		t.Error("a failed flow list closed the listeners")
	}
}
