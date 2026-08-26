# Weavster — a message-oriented integration platform.

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

## What exists now

- **Control plane** (`internal/`): API gateway (REST + OpenAPI 3.1, CSRF/security headers, TLS),
  local auth (Argon2id, password policy, lockout, anti-enumeration, MFA hook), audit log,
  scheduler (durable job queue + leases), alerts + notifier, secrets, observability
  (Prometheus + OTel, structured logs, events, stats).
- **Data plane**: data-type codecs (HL7 v2 + ACK, X12 + 997, NCPDP, JSON, XXE-safe XML,
  delimited, raw), source/sink adapters, transactional outbox with idempotency keys.
- **WASM**: YAML DSL → Go+TinyGo compiler, content-addressed signed module registry, and a
  wazero executor with resource limits (fuel/memory/time) and capability-scoped host functions.
- **State**: SQLite (local DX) / Postgres (prod) / in-memory Store with migrations, search, and
  export/import (gzip + AES-GCM).
- **Config-as-code**: YAML/JSON under a versioned root with JSON-Schema validation, plan/apply,
  and drift detection; native Git-backed store.
- **Legacy migration**: three-phase ETL (`extract → transform → load`) with dry-run report.
- **CLI** (`cmd/weavster`): server + scriptable shell (`-s` batch mode) + `weavster test`
  (JUnit/JSON output). Cross-compiles to linux/amd64, linux/arm64, darwin/arm64.

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
