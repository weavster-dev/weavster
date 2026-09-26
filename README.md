# Weavster — a message-oriented integration platform.

![Coverage](https://raw.githubusercontent.com/weavster-dev/weavster/main/docs/coverage.svg)

Config-driven, message-oriented integration platform: receive messages from files, HTTP,
TCP/MLLP, databases, SMTP, and web services; filter and transform them with declarative YAML
DSL or sandboxed WASM; and route them to one or more destinations — with durable storage,
search, export, scheduling, alerting, and a REST API + read-only topology graph.

Single static Go binary (no CGo, no external runtime).

## Quick Start

Get the server running locally in under 5 minutes.

**Prerequisites:** Go `>= 1.22` and `git`.

```bash
git clone https://github.com/weavster-dev/weavster.git
cd weavster
go build -o bin/weavster ./cmd/weavster
./bin/weavster server 0.0.0.0:8080
```

Verify it's up — the server exposes its OpenAPI contract without auth:

```bash
curl -s http://localhost:8080/api/openapi.yaml | head -n 5
```

and the system status endpoint (API routes require the CSRF marker header):

```bash
curl -s -H 'X-Weavster-CSRF: 1' http://localhost:8080/api/v1/system
```

Run the test suite to confirm a healthy checkout:

```bash
go test -race ./...
```

Full developer workflow and quality gates: see [CONTRIBUTING.md](CONTRIBUTING.md).

## What's composed & runnable

These features are wired into the running binary (`weavster server`):

- **API Gateway**: REST + OpenAPI 3.1, CSRF marker enforcement, security headers
  (HSTS, X-Frame-Options, CSP, X-Content-Type-Options), TRACE/TRACK blocking.
- **Flow CRUD**: Create, list, get, and delete flows via REST API.
- **Topology**: Read-only flow topology overview + flow-internal graph endpoints.
- **State**: In-memory message store with search.
- **Observability**: Structured logging (slog), system status endpoint.
- **CLI**: `weavster server`, basic shell (help, status, version, flow list, user list),
  `weavster test` (4 codec round-trip fixtures, JUnit/JSON output),
  `-a/-u/-p/-s/-v/-c/-h/-d` flags, batch `-s` mode.
- **Cross-compilation**: `linux/amd64`, `linux/arm64`, `darwin/arm64` (zero CGo).

## Implemented as packages

These packages have passing tests (≥87% coverage) but are not yet wired into the
composition root or need integration work:

- **Auth**: Local auth provider (Argon2id, password policy, lockout,
  anti-enumeration, MFA hook interface). Instantiated in the composition root
  but not yet invoked by request handlers — no login endpoint or auth
  middleware is active.
- **Audit**: Local audit sink with PHI access logging. Passed into gateway
  config but no handler records audit events yet.
- **Scheduler**: Durable job queue with leases, cron/interval schedules.
- **Adapters**: File, HTTP, TCP/MLLP, database, SMTP, web-service, document,
  interflow sources/sinks.
- **Outbox**: Transactional outbox + idempotency keys + retries + dead-letter.
- **WASM layer**: YAML DSL compiler (Go+TinyGo), content-addressed signed module
  registry, wazero executor with resource limits.
- **State Manager**: SQLite and Postgres backends, migrations, export/import
  with gzip + AES-GCM encryption.
- **Config-as-Code**: YAML/JSON config with JSON-Schema validation, plan/apply/drift,
  Git-backed store.
- **Alerts + Notifier**: Alert definitions, evaluation, SMTP/webhook delivery.
- **Secrets**: Local credential store + env provider, KMS interface.
- **Codecs**: HL7 v2 (+ACK), X12 (+997), NCPDP, JSON, XXE-safe XML, delimited, raw.
- **Legacy Migration**: Three-phase ETL with dry-run report.

## Enterprise-deferred

Port stubs returning `NOT_IMPLEMENTED`: DICOM source/sink/codec, broker/queue
adapters, OIDC/SAML, OPA/Cedar ABAC, SIEM audit, K8s horizontal scaling,
Redis/NATS, distributed tracing.

## Feature Matrix

| Feature | Status |
|---|---|
| REST API + OpenAPI 3.1 | ✅ Wired |
| CSRF + security headers | ✅ Wired |
| Flow CRUD (create/list/get/delete) | ✅ Wired |
| Topology graph (read-only) | ✅ Wired |
| In-memory message store + search | ✅ Wired |
| System status endpoint | ✅ Wired |
| `weavster test` (codec fixtures) | ✅ Wired |
| `weavster server` | ✅ Wired |
| Basic CLI shell (6 commands) | ✅ Wired |
| CLI batch mode (`-s`) | ✅ Wired |
| Cross-compilation (3 targets) | ✅ Wired |
| Local auth (password policy, lockout) | 🔧 Package |
| MFA hook interface | 🔧 Package |
| Audit logging (PHI access) | 🔧 Package |
| Scheduler (durable jobs, leases) | 🔧 Package |
| Source/sink adapters | 🔧 Package |
| Outbox + idempotency + dead-letter | 🔧 Package |
| WASM executor + compiler + registry | 🔧 Package |
| SQLite / Postgres Store backends | 🔧 Package |
| Config-as-Code (plan/apply/drift) | 🔧 Package |
| Git-backed config store | 🔧 Package |
| Alerts + notifier (SMTP/webhook) | 🔧 Package |
| Secrets (local + env providers) | 🔧 Package |
| Codecs (HL7v2/X12/NCPDP/JSON/XML/delimited/raw) | 🔧 Package |
| Legacy import ETL | 🔧 Package |
| TLS/HTTPS | 🔧 Package |
| Prometheus + OTel metrics | 🔧 Package |
| Statistics + time-series | 🔧 Package |
| Event log | 🔧 Package |
| DICOM source/sink/codec | ⏳ Enterprise |
| Broker/queue adapters | ⏳ Enterprise |
| OIDC/SAML SSO | ⏳ Enterprise |
| OPA/Cedar ABAC | ⏳ Enterprise |
| SIEM audit | ⏳ Enterprise |
| K8s horizontal scaling | ⏳ Enterprise |
| Redis/NATS queue | ⏳ Enterprise |
| Distributed tracing + replay | ⏳ Enterprise |

✅ = composed into binary  ·  🔧 = package exists, not yet wired  ·  ⏳ = port stub (Enterprise)

## Codec Coverage

| Codec | Parse | Serialize | ACK | Versions | Notes |
|---|---|---|---|---|---|
| HL7 v2 | ✅ | ✅ | ✅ | 2.x (MSH/MSA) | Delimiters, escaping, repetitions |
| X12 | ✅ | ✅ | ✅ (997) | ISA/GS/ST envelope | Envelopes, loops |
| NCPDP | ✅ | ✅ | — | Telecommunication | FS/GS/RS delimiters, fixed-width amounts; response limited |
| JSON | ✅ | ✅ | — | RFC 8259 | stdlib encoding/json |
| XML | ✅ | ✅ | — | 1.0 | XXE/DTD disabled by construction |
| Delimited | ✅ | ✅ | — | Configurable delimiter | Tab/pipe/comma, optional header |
| Raw/Binary | ✅ | ✅ | — | Any binary | Passthrough |
| DICOM | ⏳ | ⏳ | ⏳ | — | Enterprise (licensed library) |

## Build

```bash
go build -o bin/weavster ./cmd/weavster
go test -race ./...
weavster test --format junit --output artifacts/
```

## Run

```bash
weavster server 0.0.0.0:8080
```

## Layout

```
cmd/weavster/    entrypoint + CLI shell + composition root
internal/        private modules (hexagonal ports & adapters)
agent-docs/      OpenAPI 3.1 contract + JSON Schemas + llms.txt
iac/             Terraform / Pulumi sample modules
specs/           Phase 1/2 requirements and architecture
```

## Stack

Go (>=1.22) · `net/http` + chi · wazero (WASM) · Go + TinyGo codegen ·
PostgreSQL / SQLite / in-memory · REST + OpenAPI 3.1 · Prometheus + OTel.
