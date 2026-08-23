# AGENTS.md

Coding guidelines for the **Weavster** repository — a Go, single-binary message-oriented
integration platform. These rules apply to coding agents working in this repo.

## Source of truth (read before writing code)

- `agentic-manifest.json` — machine-readable build plan (20 modules, dependencies, acceptance criteria, frameworks). **Executable source of truth** for build-loop subagents.
- `docs/agent-onboarding.md` — non-ambiguous coding rules: language, linters, test/build/container commands, folder layout, ports.
- `docs/mvp-project-plan.md` — narrative MVP plan (scope, stack, build sequence).
- `specs/` — Phase 2 requirements and architecture (the semantics).

Precedence on conflict: the manifest wins for structure, the specs win for semantics, and
`docs/agent-onboarding.md` wins for tooling. **Surface a conflict — never resolve it silently.**

## Think before coding

- State assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them — don't pick silently.
- If something is unclear, stop and name what's confusing.

## Simplicity first

- Minimum code that solves the problem. Nothing speculative.
- No abstractions for single-use code; no unrequested configurability.
- If 200 lines could be 50, rewrite.

## Surgical changes

- Touch only what the task requires. Don't refactor adjacent code.
- Match existing style even if you'd do it differently.
- Mention unrelated issues; don't fix them unasked. Clean up only orphans *your* change created.

## Goal-driven execution

- Define success criteria; loop until verified.
- For multi-step tasks, state a plan with a `→ verify:` per step.

## Go-specific rules

- **No CGo.** The binary must cross-compile to `linux/amd64`, `linux/arm64`, `darwin/arm64` as a single static binary. `import "C"` is a build failure.
- **Hexagonal (ports & adapters).** A component depends only on a Go interface (port), never another component's concrete type. Interface definitions live in the consuming package.
- **Layout.** `cmd/weavster` (entrypoint) · `internal/<module>` (private) · `pkg/` (exported libs only).
- **Gates before a module is "done":**
  ```bash
  gofmt -l .          # MUST print nothing
  go vet ./...        # MUST pass clean
  golangci-lint run   # MUST pass (fix, don't //nolint)
  go test -race ./... # MUST pass
  ```
- **Tests.** Go `testing` package, table-driven. Never require Postgres in tests — use SQLite in-memory (`:memory:`).

## Changelog & README

- Each commit adds an entry to `CHANGELOG.md` under `[Unreleased]` (Added/Changed/Fixed/Removed).
- `README.md` reflects what exists now — never aspirational. No roadmap/"coming soon" in the README.

## Coverage

- Tests cover all acceptance criteria; generated code reaches 90% unit-test coverage.
