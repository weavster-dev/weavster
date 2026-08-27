# AGENTS.md

Coding guidelines for the **Weavster** repository — a Go, single-binary message-oriented
integration platform. These rules apply to coding agents working in this repo.

Behavioral guidelines to reduce common LLM coding mistakes. Merge with project-specific instructions as needed.

**Tradeoff:** These guidelines bias toward caution over speed. For trivial tasks, use judgment.

## 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:

- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

## 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

## 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:

- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it - don't delete it.

When your changes create orphans:

- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

## 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:

- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:

```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

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

- Tests cover all acceptance criteria; generated code reaches at least 90% unit-test coverage.

## Documentation

**Write user-facing documentation, not code documentation.** The docs site (`docs/`) explains how
a user/operator uses the tool — never how the code is structured.

- **What to document:** CLI flags/subcommands, config file YAML/JSON keys and values, API endpoints
  and request/response shapes, UI screens and workflows, adapter/connector setup.
- **Each feature gets:** a concrete example (with command-line invocation, config snippet, or API call),
  expected output/behavior, and notes on common pitfalls.
- **No godocs, no architecture diagrams, no interface descriptions.** Those belong in Go doc
  comments and ADRs, not the user docs site.
- **Docs live alongside code changes.** A PR that adds a CLI flag also adds the flag's doc entry,
  with example invocation.

Precedence: `docs/agent-onboarding.md` and `docs/mvp-project-plan.md` describe *how the docs site
is built* (MkDocs, theme, versioning). This section describes *what goes into it*.
