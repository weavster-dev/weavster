# Contributing to Weavster

Thanks for contributing! This guide covers how to set up, build, test, and submit
changes to Weavster — a single-binary, message-oriented integration platform written in Go.

Before writing code, read these (in order of precedence on conflict):

1. `agentic-manifest.json` — the machine-readable build plan (structure).
2. `specs/` — requirements and architecture (semantics).
3. `docs/agent-onboarding.md` — non-ambiguous coding rules (tooling).

If these conflict, **surface the conflict — never resolve it silently.** Open an issue.

## Prerequisites

- Go `>= 1.22`
- `golangci-lint` (uses the repo's `.golangci.yml`)

## Setup

```bash
git clone https://github.com/weavster-dev/weavster.git
cd weavster
go build -o bin/weavster ./cmd/weavster
```

## Build & run

```bash
go build -o bin/weavster ./cmd/weavster    # local build
weavster server 0.0.0.0:8080               # start the server
weavster test --format junit --output artifacts/
```

The binary must cross-compile to `linux/amd64`, `linux/arm64`, and `darwin/arm64`
as a single static binary with **zero CGo**:

```bash
GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/weavster
GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/weavster
GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/weavster
```

## Hard rules

- **No CGo.** `import "C"` is a build failure.
- **Hexagonal (ports & adapters).** Components depend only on Go interfaces (ports),
  never another component's concrete type. Interface definitions live in the consuming package.
- **Layout.** `cmd/weavster` (entrypoint) · `internal/<module>` (private) · `pkg/` (exported libs only).
- **Simplicity first.** Minimum code that solves the problem; no speculative abstractions.
- **Surgical changes.** Touch only what the task requires; match existing style.
- **Tests never require Postgres.** Use SQLite in-memory (`:memory:`) or temp files.

## Development workflow

1. Open an issue (or pick an existing one) — use the issue templates.
2. Create a branch from `main`.
3. Make the change; keep it minimal and focused.
4. Run the full gate before opening a PR:

   ```bash
   gofmt -l .                 # MUST print nothing
   go vet ./...               # MUST pass clean
   golangci-lint run          # MUST pass (fix, don't //nolint)
   go test -race ./...        # MUST pass
   ```

5. Add a `CHANGELOG.md` entry under `[Unreleased]` (Added/Changed/Fixed/Removed).
6. Ensure `README.md` reflects what exists now — never aspirational.
7. Open a pull request using the PR template and reference the issue (`Closes #N`).

## Commit style

Lowercase imperative summary, no scope noise:

```
add issue templates
fix HL7 v2 segment parsing
```

## Questions?

Open an issue or ask in the repository discussions.
