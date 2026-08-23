# Weavster

Config-driven, message-oriented integration platform — a single static Go binary
(no CGo, no external runtime).

## Reference

- [MVP Project Plan](mvp-project-plan.md) — scope, stack, build sequence
- [Agent Onboarding](agent-onboarding.md) — coding rules for contributors and agents

## What exists now

The MVP source tree is implemented: control plane (gateway, auth, audit, scheduler, alerts,
notify, secrets, observability), data plane (codecs, adapters, outbox), WASM layer (compiler,
registry, executor), durable state (SQLite/Postgres/in-memory), config-as-code + Git store,
legacy migration ETL, and the CLI/server entrypoint. See `README.md` for a full map.

## Contracts

- `agent-docs/openapi.yaml` — REST + OpenAPI 3.1 contract
- `agent-docs/schemas/config.schema.json` — config-as-code JSON Schema
- `agent-docs/schemas/transform.schema.json` — transform DSL JSON Schema
