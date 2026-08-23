package compiler

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// PinnedTinyGoVersion is the pinned, reproducible TinyGo toolchain version
// used to compile DSL transforms to WASI (arch §10.1). Same Go lineage as the
// control plane; the toolchain itself is external.
const PinnedTinyGoVersion = "0.31.0"

// Build compiles generated Go source to a WASI module with the pinned TinyGo
// toolchain, returning the .wasm bytes (a cached, rebuildable artifact).
func Build(ctx context.Context, src []byte, workdir string) ([]byte, error) {
	tinygo, err := exec.LookPath("tinygo")
	if err != nil {
		return nil, fmt.Errorf("compiler: tinygo not found (pinned %s): %w", PinnedTinyGoVersion, err)
	}
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		return nil, err
	}
	mainFile := filepath.Join(workdir, "main.go")
	if err := os.WriteFile(mainFile, src, 0o644); err != nil {
		return nil, err
	}
	outWasm := filepath.Join(workdir, "module.wasm")
	cmd := exec.CommandContext(ctx, tinygo, "build", "-target=wasi", "-o", outWasm, workdir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("compiler: tinygo build: %w: %s", err, out)
	}
	return os.ReadFile(outWasm)
}
