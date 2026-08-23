# Weavster

Config-driven, message-oriented integration platform.

This is the greenfield Go build of Weavster. The repository currently holds the Phase 2
planning artifacts and the build manifest; the Go source tree is produced by the build loop
that follows.

## What exists now

- `specs/` — Phase 2 requirements and architecture (clean-room source of truth).
- `agentic-manifest.json` — machine-readable build plan (20 modules, acceptance criteria, dependencies).
- `docs/mvp-project-plan.md` — narrative MVP plan (scope, stack, build sequence).
- `docs/agent-onboarding.md` — coding rules (language, linters, test/build commands, ports).
- `agent-docs/` — OpenAPI + JSON Schemas + `llms.txt` (skeletons).
- CI workflow (`.github/workflows/ci.yml`) and MkDocs site scaffold (`mkdocs.yml`).

## Stack (locked in Phase 2)

Go (>=1.22) · `net/http` + chi · wazero (WASM) · Go + TinyGo codegen ·
PostgreSQL / SQLite / in-memory · REST + OpenAPI 3.1 · MkDocs.
