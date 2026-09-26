# Weavster

Config-driven, message-oriented integration platform — a single static Go binary
(no CGo, no external runtime).

## Reference

- [Support matrix](support-matrix.md) — what the binary does today
- [MVP Project Plan](mvp-project-plan.md) — scope, stack, build sequence
- [Agent Onboarding](agent-onboarding.md) — coding rules for contributors and agents

## What exists now

Weavster is under active development toward its MVP. **The server does not process messages
yet.** It serves a plain-HTTP, unauthenticated REST API with in-memory flow records, a
read-only topology view of them, and a system status endpoint. Most platform capabilities
(adapters, transforms, durable storage, scheduler, auth enforcement) exist only as libraries
in the source tree.

See the [support matrix](support-matrix.md) for exactly what is wired, library-only,
Enterprise-deferred, or unsupported.

## Contracts

- `agent-docs/openapi.yaml` — REST + OpenAPI 3.1 contract
- `agent-docs/schemas/config.schema.json` — config-as-code JSON Schema
- `agent-docs/schemas/transform.schema.json` — transform DSL JSON Schema
