# CLAUDE.md — Weavster

This project's full coding guidelines live in **AGENTS.md**. Read it before writing any code:

<expand>AGENTS.md</expand>

### Quick summary

- **Go single-binary** message-oriented integration platform — **no CGo**, must cross-compile to `linux/amd64`, `linux/arm64`, `darwin/arm64`.
- **Hexagonal architecture** — depend only on Go interfaces (ports), never concrete types. Interfaces live in the consuming package.
- **Layout:** `cmd/weavster` (entrypoint) · `internal/<module>` (private) · `pkg/` (exported libs only).
- **Source-of-truth files:** `agentic-manifest.json` (build plan), `docs/agent-onboarding.md` (tooling rules), `docs/mvp-project-plan.md` (scope), `specs/` (semantics).
- **Gates:** `gofmt -l .` → nothing · `go vet ./...` → clean · `golangci-lint run` → clean · `go test -race ./...` → clean.
- **Tests:** Go `testing` package, table-driven, SQLite in-memory (never Postgres).
- **Changelog:** each commit adds to `CHANGELOG.md` under `[Unreleased]`.