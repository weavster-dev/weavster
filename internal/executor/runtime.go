package executor

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/tetratelabs/wazero"
)

// inputBase is the fixed guest-memory address where the host writes input.
const inputBase uint32 = 8

// run instantiates the compiled module, calls transform, and returns the
// output bytes.
func run(ctx context.Context, rt wazero.Runtime, compiled wazero.CompiledModule, req Request, limitType LimitType) ([]byte, error) {
	var stdio bytes.Buffer
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().
		WithName(req.ModuleName).
		WithStdout(&stdio).
		WithStderr(&stdio).
		WithStartFunctions())
	if err != nil {
		return nil, wrapRunErr(ctx, req, limitType, err)
	}

	fn := mod.ExportedFunction("transform")
	if fn == nil {
		return nil, fmt.Errorf("executor: module %s@%s does not export transform", req.ModuleName, req.Version)
	}

	mem := mod.Memory()
	if mem == nil {
		return nil, fmt.Errorf("executor: module %s@%s has no memory", req.ModuleName, req.Version)
	}
	if !mem.Write(inputBase, req.Input) {
		return nil, fmt.Errorf("executor: write input to guest memory for %s@%s", req.ModuleName, req.Version)
	}

	results, err := fn.Call(ctx, uint64(inputBase), uint64(len(req.Input)))
	if err != nil {
		return nil, wrapRunErr(ctx, req, limitType, err)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("executor: transform for %s@%s returned no result", req.ModuleName, req.Version)
	}

	out, ok := mem.Read(inputBase, uint32(results[0]))
	if !ok {
		return nil, fmt.Errorf("executor: read output from guest memory for %s@%s", req.ModuleName, req.Version)
	}
	return out, nil
}

func wrapRunErr(ctx context.Context, req Request, limitType LimitType, err error) error {
	if limitType != "" && isDeadline(err) {
		return &LimitError{Module: req.ModuleName, Version: req.Version, InputHash: req.InputHash(), Limit: limitType, Err: err}
	}
	if req.Limits.MemoryPages > 0 && isMemoryError(err) {
		return &LimitError{Module: req.ModuleName, Version: req.Version, InputHash: req.InputHash(), Limit: LimitMemory, Err: err}
	}
	return fmt.Errorf("executor: %s@%s: %w", req.ModuleName, req.Version, err)
}

func isMemoryError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "memory") || strings.Contains(s, "exceeds")
}
