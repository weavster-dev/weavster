package executor

import (
	"context"
	"crypto/sha256"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// Capability names for host functions (arch §4.2).
const (
	CapParse     = "parse"
	CapSerialize = "serialize"
	CapAck       = "ack"
	CapRoute     = "route"
	CapStore     = "store"
	CapNet       = "net"
	CapCrypto    = "crypto"
)

// HostModule is the module name guests import host functions from.
const HostModule = "weavster"

// Router is consumed by the route host function for in-process inter-flow
// routing (spec §2.3.9). Defined in the consuming package (hexagonal).
type Router interface {
	Route(ctx context.Context, name string, msg []byte) error
}

type hostRegistry struct {
	router Router
}

// register instantiates the host module exposing only the functions whose
// capability was declared (least privilege, arch §4.2).
func (h *hostRegistry) register(ctx context.Context, rt wazero.Runtime, caps []string) (api.Module, error) {
	allowed := make(map[string]bool, len(caps))
	for _, c := range caps {
		allowed[c] = true
	}
	b := rt.NewHostModuleBuilder(HostModule)

	if allowed[CapRoute] {
		b.NewFunctionBuilder().WithFunc(h.route).Export("route")
	}
	if allowed[CapCrypto] {
		b.NewFunctionBuilder().WithFunc(h.hashSha256).Export("hash_sha256")
	}
	for _, c := range []struct {
		cap  string
		name string
	}{
		{CapParse, "parse"},
		{CapSerialize, "serialize"},
		{CapAck, "ack"},
		{CapStore, "store"},
		{CapNet, "net"},
	} {
		if allowed[c.cap] {
			b.NewFunctionBuilder().WithFunc(stubI32x2).Export(c.name)
		}
	}
	return b.Instantiate(ctx)
}

// route implements inter-flow routing: (namePtr, nameLen, msgPtr, msgLen) ->
// status (0 = ok).
func (h *hostRegistry) route(ctx context.Context, m api.Module, namePtr, nameLen, msgPtr, msgLen uint32) uint32 {
	if h.router == nil {
		return 1
	}
	name, ok := m.Memory().Read(namePtr, nameLen)
	if !ok {
		return 2
	}
	msg, ok := m.Memory().Read(msgPtr, msgLen)
	if !ok {
		return 3
	}
	if err := h.router.Route(ctx, string(name), msg); err != nil {
		return 4
	}
	return 0
}

// hashSha256 computes SHA-256 of memory[ptr:ptr+len] into outPtr (32 bytes).
func (h *hostRegistry) hashSha256(_ context.Context, m api.Module, ptr, length, outPtr uint32) uint32 {
	data, ok := m.Memory().Read(ptr, length)
	if !ok {
		return 1
	}
	sum := sha256.Sum256(data)
	if !m.Memory().Write(outPtr, sum[:]) {
		return 2
	}
	return 0
}

// stubI32x2 is a not-implemented host function placeholder (status 1).
func stubI32x2(context.Context, api.Module, uint32, uint32) uint32 { return 1 }
