// Package executor implements the TransformEngine port over the wazero WASM
// runtime with resource limits and capability-scoped host functions.
package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/tetratelabs/wazero"
)

// TransformEngine is the port for sandboxed user logic (arch §3.1, §4).
type TransformEngine interface {
	Transform(ctx context.Context, req Request) ([]byte, error)
}

// Request is a single transform invocation.
type Request struct {
	ModuleName   string
	Version      string
	Wasm         []byte
	Input        []byte
	Capabilities []string
	Limits       Limits
}

// InputHash returns the SHA-256 of the input, linking errors to the message
// (gap #3 MVP).
func (r Request) InputHash() string {
	sum := sha256.Sum256(r.Input)
	return hex.EncodeToString(sum[:])
}

// Engine is the wazero TransformEngine adapter.
type Engine struct {
	host *hostRegistry
}

// NewEngine returns an engine with the given inter-flow Router (may be nil).
func NewEngine(router Router) *Engine {
	return &Engine{host: &hostRegistry{router: router}}
}

// Transform runs the module's exported "transform" function on the input with
// the configured limits and declared capabilities (least privilege). The guest
// ABI is: host writes input at a base address, calls transform(ptr, len) ->
// outLen, and reads outLen output bytes from ptr.
func (e *Engine) Transform(ctx context.Context, req Request) ([]byte, error) {
	execCtx, cancel, limitType := req.Limits.withDeadline(ctx)
	defer cancel()

	rConfig := wazero.NewRuntimeConfigInterpreter().WithCloseOnContextDone(true)
	if req.Limits.MemoryPages > 0 {
		rConfig = rConfig.WithMemoryLimitPages(req.Limits.MemoryPages)
	}
	rt := wazero.NewRuntimeWithConfig(execCtx, rConfig)
	defer func() { _ = rt.Close(execCtx) }()

	compiled, err := rt.CompileModule(execCtx, req.Wasm)
	if err != nil {
		return nil, fmt.Errorf("executor: compile %s@%s: %w", req.ModuleName, req.Version, err)
	}

	// Register host functions per declared capabilities (least privilege).
	if _, err := e.host.register(execCtx, rt, req.Capabilities); err != nil {
		return nil, fmt.Errorf("executor: host functions for %s@%s: %w", req.ModuleName, req.Version, err)
	}

	return run(execCtx, rt, compiled, req, limitType)
}

var _ TransformEngine = (*Engine)(nil)
