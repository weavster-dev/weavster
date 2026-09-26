# Weavster

Config-driven, message-oriented integration platform — a single static Go binary
(no CGo, no external runtime).

## Reference

- [Support matrix](support-matrix.md) — what the binary does today
- [MVP Project Plan](mvp-project-plan.md) — scope, stack, build sequence
- [Agent Onboarding](agent-onboarding.md) — coding rules for contributors and agents

## What exists now

The server serves an authenticated REST API (HTTP, plus HTTPS when configured). Through it you
define flows, send messages into them, transform messages with the YAML DSL, and deliver them
to HTTP and file destinations with automatic retries; every message is stored with its status. See
[Processing messages](processing-messages.md). Listening sources, scheduling, and WASM modules exist only as
libraries in the source tree.

See the [support matrix](support-matrix.md) for exactly what is wired, library-only,
Enterprise-deferred, or unsupported.

## Contracts

- `agent-docs/openapi.yaml` — REST + OpenAPI 3.1 contract
- `agent-docs/schemas/config.schema.json` — config-as-code JSON Schema (its flows refer to `flow.schema.json`)
- `agent-docs/schemas/transform.schema.json` — transform DSL JSON Schema
