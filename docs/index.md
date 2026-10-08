# Weavster

Config-driven, message-oriented integration platform — a single static Go binary
(no CGo, no external runtime).

New here? [Getting started](getting-started.md) takes you from nothing to a processed message in
about ten minutes.

## Reference

- [Support matrix](support-matrix.md) — what the binary does today

## What exists now

The server serves an authenticated REST API (HTTP, plus HTTPS when configured). Through it you
define flows, send messages into them, transform messages with the YAML DSL, and deliver them
to HTTP, file, and MLLP (HL7 v2) destinations with automatic retries; every message is stored with its status. See
[Processing messages](processing-messages.md). A flow can also read files from a directory,
listen on its own port for HTTP requests or HL7 v2 over MLLP, or poll a database query.

See the [support matrix](support-matrix.md) for exactly what is wired, library-only,
Enterprise-deferred, or unsupported.

## Contracts

- `agent-docs/openapi.yaml` — REST + OpenAPI 3.1 contract, identical to the one the server serves at `GET /api/openapi.yaml`
- `agent-docs/schemas/config.schema.json` — config-as-code JSON Schema (its flows refer to `flow.schema.json`)
- `agent-docs/schemas/transform.schema.json` — transform DSL JSON Schema
