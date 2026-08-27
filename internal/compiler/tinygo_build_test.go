package compiler

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFakeTinyGo installs an executable "tinygo" script into a temp dir and
// points PATH at it, so exec.LookPath("tinygo") succeeds while still letting
// the test control every downstream behavior of Build without a real toolchain.
func writeFakeTinyGo(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "tinygo")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake tinygo: %v", err)
	}
	t.Setenv("PATH", dir)
}

// noopTinyGo is a minimal fake that exits 0 immediately. It is only used for
// branches that fail before invoking the toolchain.
const noopTinyGo = "#!/bin/sh\nexit 0\n"

func TestBuildMkdirAllError(t *testing.T) {
	writeFakeTinyGo(t, noopTinyGo)

	// Parent of workdir is a regular file, so os.MkdirAll fails with ENOTDIR.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Build(context.Background(), []byte("package main"), filepath.Join(blocker, "sub"))
	if err == nil {
		t.Fatal("expected MkdirAll error when workdir parent is a file")
	}
	if strings.Contains(err.Error(), "tinygo not found") {
		t.Fatalf("unexpected LookPath error: %v", err)
	}
}

func TestBuildWriteFileError(t *testing.T) {
	writeFakeTinyGo(t, noopTinyGo)

	workdir := t.TempDir()
	// main.go is a directory, so os.WriteFile fails with EISDIR.
	if err := os.MkdirAll(filepath.Join(workdir, "main.go"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Build(context.Background(), []byte("package main"), workdir)
	if err == nil {
		t.Fatal("expected WriteFile error when main.go is a directory")
	}
}

func TestBuildTinyGoBuildFailure(t *testing.T) {
	writeFakeTinyGo(t, "#!/bin/sh\necho 'compile error: boom' >&2\nexit 1\n")

	_, err := Build(context.Background(), []byte("package main"), t.TempDir())
	if err == nil {
		t.Fatal("expected error when tinygo build exits non-zero")
	}
	if !strings.Contains(err.Error(), "tinygo build") {
		t.Fatalf("error should wrap tinygo build failure, got: %v", err)
	}
	if !strings.Contains(err.Error(), "compile error: boom") {
		t.Fatalf("error should include toolchain output, got: %v", err)
	}
}

func TestBuildSuccess(t *testing.T) {
	// The fake toolchain writes the .wasm to the -o argument and exits 0,
	// mirroring a real tinygo build of a WASI module.
	writeFakeTinyGo(t, `#!/bin/sh
prev=""
out=""
for arg in "$@"; do
  if [ "$prev" = "-o" ]; then out="$arg"; fi
  prev="$arg"
done
printf 'fake-wasm-bytes' > "$out"
exit 0
`)

	workdir := t.TempDir()
	got, err := Build(context.Background(), []byte("package main"), workdir)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if string(got) != "fake-wasm-bytes" {
		t.Fatalf("build returned %q, want %q", got, "fake-wasm-bytes")
	}
}
