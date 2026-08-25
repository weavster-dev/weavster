package executor

import (
	"context"
	"errors"
	"testing"

	"github.com/tetratelabs/wazero"
)

// memOnlyWasm is a minimal module that exports linear memory only, with no
// functions. It lets tests call host functions directly (as the wazero
// runtime would when a guest imports them) without hand-assembling a guest
// module for every capability.
var memOnlyWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x05, 0x03, 0x01, 0x00, 0x01,
	0x07, 0x0a, 0x01, 0x06, 'm', 'e', 'm', 'o', 'r', 'y', 0x02, 0x00,
}

type fakeRouter struct {
	name   string
	msg    []byte
	err    error
	called bool
}

func (f *fakeRouter) Route(_ context.Context, name string, msg []byte) error {
	f.called = true
	f.name = name
	f.msg = append([]byte(nil), msg...)
	return f.err
}

func TestHostRegistryRoute(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigInterpreter())
	defer func() { _ = rt.Close(ctx) }()
	mod, err := rt.Instantiate(ctx, memOnlyWasm)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer func() { _ = mod.Close(ctx) }()

	mem := mod.Memory()
	if mem == nil {
		t.Fatal("expected exported memory")
	}
	if !mem.Write(0, []byte("myflow")) {
		t.Fatal("write name")
	}
	if !mem.Write(64, []byte("payload")) {
		t.Fatal("write msg")
	}

	t.Run("nil router returns 1", func(t *testing.T) {
		h := &hostRegistry{}
		if got := h.route(ctx, mod, 0, 6, 64, 7); got != 1 {
			t.Errorf("route with nil router = %d, want 1", got)
		}
	})

	t.Run("bad name pointer returns 2", func(t *testing.T) {
		h := &hostRegistry{router: &fakeRouter{}}
		if got := h.route(ctx, mod, 1<<20, 6, 64, 7); got != 2 {
			t.Errorf("route with bad name ptr = %d, want 2", got)
		}
	})

	t.Run("bad msg pointer returns 3", func(t *testing.T) {
		h := &hostRegistry{router: &fakeRouter{}}
		if got := h.route(ctx, mod, 0, 6, 1<<20, 7); got != 3 {
			t.Errorf("route with bad msg ptr = %d, want 3", got)
		}
	})

	t.Run("router error returns 4", func(t *testing.T) {
		fr := &fakeRouter{err: errors.New("boom")}
		h := &hostRegistry{router: fr}
		if got := h.route(ctx, mod, 0, 6, 64, 7); got != 4 {
			t.Errorf("route with router error = %d, want 4", got)
		}
		if !fr.called {
			t.Error("expected router.Route to be called")
		}
	})

	t.Run("success returns 0 and forwards name/msg", func(t *testing.T) {
		fr := &fakeRouter{}
		h := &hostRegistry{router: fr}
		if got := h.route(ctx, mod, 0, 6, 64, 7); got != 0 {
			t.Errorf("route success = %d, want 0", got)
		}
		if fr.name != "myflow" {
			t.Errorf("routed name = %q, want myflow", fr.name)
		}
		if string(fr.msg) != "payload" {
			t.Errorf("routed msg = %q, want payload", fr.msg)
		}
	})
}

func TestStubI32x2(t *testing.T) {
	// stubI32x2 backs parse/serialize/ack/store/net capabilities that have no
	// MVP implementation yet; it must always report "not implemented" (1) and
	// never panic regardless of arguments.
	if got := stubI32x2(context.Background(), nil, 123, 456); got != 1 {
		t.Errorf("stubI32x2 = %d, want 1", got)
	}
}

func TestHostRegistryRegisterGatesUnstubbedCapabilities(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigInterpreter())
	defer func() { _ = rt.Close(ctx) }()

	h := &hostRegistry{}
	mod, err := h.register(ctx, rt, []string{CapParse, CapStore, CapNet})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer func() { _ = mod.Close(ctx) }()

	for _, name := range []string{"parse", "store", "net"} {
		if mod.ExportedFunction(name) == nil {
			t.Errorf("expected stubbed export %q to be registered", name)
		}
	}
	if mod.ExportedFunction("route") != nil {
		t.Error("route should not be registered without CapRoute")
	}
	if mod.ExportedFunction("hash_sha256") != nil {
		t.Error("hash_sha256 should not be registered without CapCrypto")
	}
}
