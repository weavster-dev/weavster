# Agent Onboarding — Coding Rules for the Build Loop (Prompt 3)

> **Audience:** coding subagents implementing `agentic-manifest.json`.
> **Source of truth:** `specs/target-architecture-wasm-iac.md`, `specs/black-box-functional-spec.md`, `specs/production-viability-gap-analysis.md`, `specs/read-only-graph-view-contract.md`.
> **Purpose:** remove every guess. If a rule is not stated here, do not invent one — open a question.

---

## 1. Language & Module (non-negotiable)

| Item | Value |
|---|---|
| Language | **Go** (`>=1.22`) |
| Module path | `github.com/weavster-dev/weavster` |
| Binary name | `weavster` |
| Package manager | Go modules (`go.mod`) |
| Web framework | `net/http` + **chi** (`github.com/go-chi/chi/v5`) |
| WASM runtime | **wazero** (`github.com/tetratelabs/wazero`) |
| DSL codegen target | **Go + TinyGo → WASI** (pinned toolchain, external) |
| Databases | PostgreSQL (`pgx/v5`), SQLite (`modernc.org/sqlite` — pure-Go, **no CGo**), in-memory |

**Hard constraints:**
- **No CGo.** The binary must cross-compile to `linux/amd64`, `linux/arm64`, `darwin/arm64` as a single static binary. Any `import "C"` is a build failure.
- **Zero heavy SDKs** on the end-user host (no Rust/Java/.NET runtime). This is the Go binary only.
- **Single binary / single OCI container** (monolith-first). Internal boundaries exist only as Go interfaces.

## 2. Folder Layout (fixed)

```
weavster/                        # repo root = module root
├── cmd/
│   └── weavster/                # main + CLI shell + server wiring (composition root)
├── internal/
│   ├── gateway/                 # API Gateway (REST+OpenAPI, authN/Z, audit, TLS/CSRF/headers)
│   ├── auth/                    # AuthProvider + Authorizer, password policy, lockout, MFA hook
│   ├── audit/                   # AuditSink (PHI access logging)
│   ├── scheduler/               # cron/interval + durable job queue + reconciler + lease
│   ├── executor/                # wazero WASM runtime + resource limits + host functions
│   ├── state/                   # State Manager (Store: postgres/sqlite/memory) + migrations + search/export
│   ├── adapters/                # Source/Sink ports (file/http/tcp-MLLP/db/smtp/webservice/interflow/document)
│   ├── codecs/                  # Data-type codecs (HL7 v2, X12, NCPDP, JSON, XML, delimited, raw)
│   ├── compiler/                # YAML DSL → TinyGo → WASM codegen + schema validation
│   ├── registry/                # WASM module registry (version/sign/promote/rollback/GC)
│   ├── config/                  # config-as-code: plan/apply/drift, JSON-Schema validation
│   ├── gitstore/                # native Git-backed config store (commit/push/pull/history/restore)
│   ├── migrate/                 # legacy import ETL (extract/transform/load/dry-run)
│   ├── outbox/                  # transactional outbox + idempotency keys + retry/dead-letter
│   ├── alerts/                  # alert definitions + evaluation
│   ├── notify/                  # Notifier port (SMTP/webhook)
│   ├── secrets/                 # SecretProvider (local store + env//run/secrets)
│   ├── observability/           # metrics (Prometheus) + slog logging + OTel + events + stats
│   └── topology/                # read-only flow topology graph (MVP UI data contract)
└── pkg/                         # only intentionally-exported shared libraries (empty at MVP is fine)
```

- Every package under `internal/` is private to this module. Put nothing publicly-importable in `internal/`.
- One Go package per directory. Package name = directory name (`package gateway`, `package scheduler`, …). `cmd/weavster` is `package main`.

## 3. Architectural Law (hexagonal / ports & adapters)

**Every internal component depends only on a port (Go interface), never on another component's concrete type.** Adapters implement ports. This is constraint #6 (modularity) and is the enabler for future K8s split.

The canonical ports (from the architecture doc §3.1) are:

| Port | Consumer | Implementations (MVP) |
|---|---|---|
| `Source` / `Sink` | Executor | file, http, tcp-MLLP, in-memory, db, smtp, web-service, document |
| `Store` | State Manager callers | Postgres, SQLite, in-memory |
| `JobQueue` | Scheduler | Postgres `SKIP LOCKED`, SQLite |
| `TransformEngine` | Executor | wazero |
| `AuthProvider` | Gateway | local user store |
| `Authorizer` | Gateway | built-in permission set |
| `AuditSink` | Gateway/State | local event store |
| `SecretProvider` | adapters/TLS | local credential store, env |
| `MetricsExporter` | all | Prometheus + OTel |
| `Notifier` | Alerts | SMTP, webhook |

Interface definitions live **in the consuming package** (Go idiom: "accept interfaces, return structs"). Adapters live in their own package and satisfy the interface implicitly.

## 4. Testing

- **Test framework:** Go standard `testing` package. Files named `*_test.go` in the same package.
- **Unit test command (the review-loop gate):**
  ```bash
  go test -race ./...
  ```
- **Per-module gate** (used in manifest acceptance criteria), e.g.:
  ```bash
  go test -race ./internal/executor/...
  ```
- **Flow-level tests** (end-user transform fixtures) run through the binary, never by hand-rolling fixtures in unit tests:
  ```bash
  weavster test --format junit --output artifacts/
  ```
- **Table-driven tests** are the Go idiom — prefer them. WASM executors MUST have a `.wasm` fixture or a build step that compiles a TinyGo sample under a pinned toolchain (checked in or produced by `make fixtures`).
- Any test that needs a database MUST use SQLite (in-memory `:memory:` or temp file) — **never require Postgres** for the test suite (constraint #3).

## 5. Lint / Format / Vet (run before declaring a module done)

```bash
gofmt -l .                 # MUST print nothing
go vet ./...               # MUST pass clean
golangci-lint run          # MUST pass (use the repo .golangci.yml)
```

`gofmt` output is the formatting truth — do not hand-format. `go vet` catches real bugs. `golangci-lint` is the review-loop linter; fix all findings, do not `//nolint` without a written reason.

## 6. Build & Package

```bash
go build -o bin/weavster ./cmd/weavster       # local build
go build -ldflags="-s -w" ./cmd/weavster      # stripped release build
```

**Cross-compile (must succeed, zero CGo):**
```bash
GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -o bin/weavster-linux-amd64   ./cmd/weavster
GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -o bin/weavster-linux-arm64   ./cmd/weavster
GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -o bin/weavster-darwin-arm64  ./cmd/weavster
```

**Container (distroless, non-root):**
```bash
docker build -t weavster:latest .
```
The `Dockerfile` MUST be multi-stage (`golang:1.22` → `gcr.io/distroless/static-debian12:nonroot`), run as non-root, and contain only the static binary + the `agent-docs/` assets.

## 7. Version Control of Your Work

- Go module deps go in `go.mod`/`go.sum` via `go get` / `go mod tidy`. Never hand-edit `go.sum`.
- Do not commit build artifacts (`bin/`, compiled `.wasm` caches) — they are derived. YAML is the source of truth for transforms.
- Follow the repo's existing commit style (lowercase imperative summary, no scope noise).

## 8. What You Are NOT Allowed to Do

- ❌ Add a second language (no Rust, no Python, no Node runtime). TinyGo/WASI is the only guest path.
- ❌ Introduce CGo or any c-shared dependency.
- ❌ Bypass a port and depend on a concrete adapter across a module boundary.
- ❌ Require Postgres for any unit test or for local `weavster test` execution.
- ❌ Implement Enterprise-scoped items (SSO/OIDC-SAML, ABAC/OPA-Cedar, Redis/NATS queue, leader election, DICOM SCU/SCP, distributed tracing, object-store blobs, multi-tenancy). **Leave the interface, stub/`NOT_IMPLEMENTED` the rest.**
- ❌ Add features beyond the manifest's `acceptance_criteria` for a module.

## 9. Acceptance Loop

For each module in `agentic-manifest.json`:
1. Create the `files_to_create` in the given `path` with the given `frameworks_to_use`.
2. Implement against the `acceptance_criteria` (trace each criterion to a spec use-case).
3. Run `gofmt -l .` (empty), `go vet ./...` (clean), `golangci-lint run` (clean).
4. Run `go test -race ./<module-path>/...` — every acceptance criterion that names a test command must pass.
5. Report per-criterion pass/fail; do not mark a module complete until its gates are green.

The manifest is the contract. The specs are the semantics. This document is the how. When they conflict, surface the conflict — do not resolve it silently.
