package executor

import (
	"context"
	"errors"
	"testing"

	"github.com/tetratelabs/wazero/api"
)

// fakeMemory is a minimal api.Memory stub that only implements Read, backed
// by a fixed byte slice. All other methods panic via the embedded nil
// interface if invoked, which is fine since route() only calls Read.
type fakeMemory struct {
	api.Memory
	data []byte
}

func (f fakeMemory) Read(offset, length uint32) ([]byte, bool) {
	start := uint64(offset)
	end := start + uint64(length)
	if end > uint64(len(f.data)) {
		return nil, false
	}
	return f.data[start:end], true
}

// fakeModule is a minimal api.Module stub exposing only Memory().
type fakeModule struct {
	api.Module
	mem api.Memory
}

func (f fakeModule) Memory() api.Memory { return f.mem }

type stubRouter struct {
	err  error
	name string
	msg  []byte
}

func (r *stubRouter) Route(_ context.Context, name string, msg []byte) error {
	r.name = name
	r.msg = msg
	return r.err
}

func TestHostRegistryRoute(t *testing.T) {
	ctx := context.Background()
	data := []byte("orders")
	data = append(data, []byte("hello")...)
	nameLen := uint32(len("orders"))
	namePtr := uint32(0)
	msgPtr := nameLen
	msgLen := uint32(len("hello"))

	mod := fakeModule{mem: fakeMemory{data: data}}

	t.Run("no router configured", func(t *testing.T) {
		h := &hostRegistry{}
		got := h.route(ctx, mod, namePtr, nameLen, msgPtr, msgLen)
		if got != 1 {
			t.Fatalf("expected status 1 with nil router, got %d", got)
		}
	})

	t.Run("name read out of bounds", func(t *testing.T) {
		h := &hostRegistry{router: &stubRouter{}}
		got := h.route(ctx, mod, namePtr, uint32(len(data))+100, msgPtr, msgLen)
		if got != 2 {
			t.Fatalf("expected status 2 for bad name read, got %d", got)
		}
	})

	t.Run("message read out of bounds", func(t *testing.T) {
		h := &hostRegistry{router: &stubRouter{}}
		got := h.route(ctx, mod, namePtr, nameLen, msgPtr, uint32(len(data))+100)
		if got != 3 {
			t.Fatalf("expected status 3 for bad message read, got %d", got)
		}
	})

	t.Run("router returns error", func(t *testing.T) {
		h := &hostRegistry{router: &stubRouter{err: errors.New("boom")}}
		got := h.route(ctx, mod, namePtr, nameLen, msgPtr, msgLen)
		if got != 4 {
			t.Fatalf("expected status 4 when router errors, got %d", got)
		}
	})

	t.Run("success routes name and message through", func(t *testing.T) {
		r := &stubRouter{}
		h := &hostRegistry{router: r}
		got := h.route(ctx, mod, namePtr, nameLen, msgPtr, msgLen)
		if got != 0 {
			t.Fatalf("expected status 0 on success, got %d", got)
		}
		if r.name != "orders" {
			t.Fatalf("expected routed name %q, got %q", "orders", r.name)
		}
		if string(r.msg) != "hello" {
			t.Fatalf("expected routed message %q, got %q", "hello", r.msg)
		}
	})
}

func TestStubI32x2(t *testing.T) {
	if got := stubI32x2(context.Background(), nil, 0, 0); got != 1 {
		t.Fatalf("expected stubI32x2 to always return 1, got %d", got)
	}
}
