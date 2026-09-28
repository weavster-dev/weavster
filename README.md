# Weavster — a message-oriented integration platform.

![Coverage](https://raw.githubusercontent.com/weavster-dev/weavster/main/docs/coverage.svg)

Message-oriented integration platform. The current server stores flow definitions, accepts
messages for a flow through its REST API, from files in a directory, on the flow's own HTTP
port, or as HL7 v2 over MLLP (acknowledged with HL7 ACKs), transforms them with a declarative YAML DSL (`map`/`set`/`filter`), and delivers them to
HTTP, file, and MLLP (HL7 v2 over TCP) destinations, recording every message and its status;
failed deliveries are retried with backoff and dead-lettered after a limit. Database sources, cron
scheduling, and WASM modules exist as libraries in the source tree that the server does not use.
See [What exists now](#what-exists-now) and the
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

On first start the server prints a one-time `admin` password to stderr (or set
`WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD`, see [Authentication](docs/authentication.md)).

> **Warning:** plain HTTP sends credentials in cleartext. Keep the cleartext listener on
> `127.0.0.1` and use an HTTPS listener (`listen.tlsAddress`, see
> [Server configuration](docs/server-config.md)) for anything reachable over a network.

Verify it's up by fetching the OpenAPI contract:

```bash
curl -s http://localhost:8080/api/openapi.yaml | head -n 5
```

and the system status endpoint (API routes require the CSRF marker header):

```bash
curl -s -u 'admin:PASSWORD' -H 'X-Weavster-CSRF: 1' http://localhost:8080/api/v1/system
```

Run the test suite to confirm a healthy checkout:

```bash
go test -race ./...
```

Full developer workflow and quality gates: see [CONTRIBUTING.md](CONTRIBUTING.md).

## What exists now

The [support matrix](docs/support-matrix.md) is the authoritative, per-capability list. In short:

- **Running server** (`weavster server [--config FILE]`): REST API over HTTP and optional HTTPS,
  with the OpenAPI document,
  `/api/v1/system`, CSRF marker enforcement, security headers, and TRACE/TRACK blocking;
  flow create/list/get/update/delete stored in the configured store (durable with `sqlite`),
  enable/disable with auto-deploy of enabled flows at startup, and a
  deploy/start/stop/pause/halt/resume/undeploy lifecycle;
  `POST /api/v1/flows/{id}/messages` runs a message through the flow's DSL transform and
  delivers it to each `http`/`file` destination, retrying failures with backoff (see
  [Processing messages](docs/processing-messages.md)); a flow can also read files from a
  directory, listen on its own HTTP port, or accept HL7 v2 over MLLP, and transforms can read
  HL7 v2 messages, XML documents, and CSV (`inputFormat: hl7v2`, `xml`, or `delimited`); message
  search (flow, status, time,
  paging), reading one message and its content (audited), reprocessing, and removing messages;
  read-only topology JSON built from the flows.
  Basic or Bearer-token authentication with per-route permissions, and a first-run `admin`
  account; security-relevant API calls are written to an audit log on stderr. Users persist
  across restarts only with `store.dialect: sqlite`; otherwise they are kept in memory.
- **Configuration management** (API and CLI): user administration; the config map, global
  scripts, and settings; code snippets and libraries; alert definitions (stored and validated;
  they do not send notifications yet); whole-configuration export and import; and checking a
  config-as-code document and planning and applying it (`config validate`, `diff`, `plan`,
  `apply`). These are stored and managed only: flows do not
  use snippets, scripts, or the config map yet.
- **CLI**: `weavster server`, `weavster test` (four built-in codec round-trip fixtures,
  JUnit/JSON output), and the command-line client: a bare `weavster` opens the interactive
  shell, and `-s` runs batch scripts, with `help`, `status`, `version`, `flow` commands for every
  flow API operation, user administration, and `quit` (see `docs/cli.md`). `-u`/`-p` log in, `-c` reads
  a connection file, and `-v` prints the server's version.
- **Library-only** (source and unit tests exist, not used by the server): durable audit storage,
  scheduler, the other adapters (database, SMTP, web service), outbox, codecs other than
  HL7 v2, XML, and delimited, WASM compiler/executor/registry, PostgreSQL
  store, config-as-code drift, Git store, alert evaluation, notifiers, secrets,
  metrics/tracing.
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
weavster server --config weavster.yaml   # see docs/server-config.md
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

Go (>=1.22) · `net/http` + chi · REST + OpenAPI 3.1 · in-memory or SQLite store. Library-only packages
also depend on wazero, SQLite/PostgreSQL drivers, Prometheus, and OpenTelemetry.
