# Weavster — a message-oriented integration platform.

![Coverage](https://raw.githubusercontent.com/weavster-dev/weavster/main/docs/coverage.svg)

Message-oriented integration platform. The current server stores flow definitions in memory,
serves them through a REST API and a read-only topology API, and does not receive, transform, or
deliver messages. Adapters, transforms, durable storage, and scheduling exist as libraries in the
source tree that the server does not use. See [What exists now](#what-exists-now) and the
[support matrix](docs/support-matrix.md).

Single static Go binary (no CGo, no external runtime).

## Quick Start

Get the server running locally in under 5 minutes.

**Prerequisites:** Go `>= 1.22` and `git`.

```bash
git clone https://github.com/weavster-dev/weavster.git
cd weavster
go build -o bin/weavster ./cmd/weavster
./bin/weavster server 127.0.0.1:8080
```

> **Warning:** API routes are unauthenticated. Keep the server on `127.0.0.1`; do not expose it
> to a network.

Verify it's up by fetching the OpenAPI contract:

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

The [support matrix](docs/support-matrix.md) is the authoritative, per-capability list. In short:

- **Running server** (`weavster server`): plain-HTTP REST API with the OpenAPI document,
  `/api/v1/system`, CSRF marker enforcement, security headers, and TRACE/TRACK blocking;
  flow create/list/get/delete backed by an **in-memory** map (lost on restart, never executed);
  read-only topology JSON built from those flows; message search that always returns `[]`.
  **API routes are unauthenticated** — no login, authorization, or audit is enforced.
- **CLI**: `weavster server`, `weavster test` (four built-in codec round-trip fixtures,
  JUnit/JSON output), and `-s` batch scripts with `help`, `status`, `version`, `flow list`,
  `user list`, `quit`. `-u`, `-p`, and `-c` are parsed but ignored; `-v` prints the local version.
- **Library-only** (source and unit tests exist, not used by the server): auth, audit,
  scheduler, adapters, outbox, codecs, WASM compiler/executor/registry, SQLite/PostgreSQL
  store, config-as-code, Git store, alerts, notifiers, secrets, metrics/tracing.
- **Enterprise-deferred stubs**: broker and DICOM adapters, DICOM codec, KMS/Vault rotation.
- **Build**: CI verifies static `CGO_ENABLED=0` builds for linux/amd64, linux/arm64,
  darwin/arm64 and a distroless non-root image. No release artifacts are published.

## Build

```bash
go build -o bin/weavster ./cmd/weavster
go test -race ./...
weavster test --format junit --output artifacts/
```

## Run

```bash
weavster server 127.0.0.1:8080
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

Go (>=1.22) · `net/http` + chi · REST + OpenAPI 3.1 · in-memory store. Library-only packages
also depend on wazero, SQLite/PostgreSQL drivers, Prometheus, and OpenTelemetry.
