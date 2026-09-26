# Weavster

Config-driven, message-oriented integration platform — a single static Go binary
(no CGo, no external runtime).

## Reference

- [MVP Project Plan](mvp-project-plan.md) — scope, stack, build sequence
- [Agent Onboarding](agent-onboarding.md) — coding rules for contributors and agents

## What exists now

**Composed into the binary:** API gateway (REST + OpenAPI 3.1, CSRF, security
headers), flow CRUD (create/list/get/delete), read-only topology, in-memory
message store with search, system status endpoint, structured logging, basic
CLI shell (6 commands), batch mode, and `weavster test` with codec
round-trip fixtures.

**Implemented as packages** (tested, pending wiring into the composition root):
auth (password policy, lockout, anti-enumeration, MFA hook — not yet invoked
by request handlers), audit (not yet called by handlers), scheduler, adapters,
outbox, WASM compiler/executor/registry, SQLite/Postgres Store backends,
config-as-code, Git store, alerts, notifier, secrets, codecs, and legacy
migration ETL.

**Enterprise-deferred:** DICOM, broker/queue adapters, SSO, ABAC, SIEM audit,
horizontal scaling, distributed tracing — port stubs returning `NOT_IMPLEMENTED`.

See [`README.md`](../README.md) for the full feature/support matrix and codec
coverage table.

## Contracts

- `agent-docs/openapi.yaml` — REST + OpenAPI 3.1 contract
- `agent-docs/schemas/config.schema.json` — config-as-code JSON Schema
- `agent-docs/schemas/transform.schema.json` — transform DSL JSON Schema
