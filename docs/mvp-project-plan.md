# Weavster MVP — Project Plan

**Status:** Draft for review
**Phase:** 2.5 (collation of Phase 2 → single MVP build plan)
**Inputs:** `specs/black-box-functional-spec.md`, `specs/target-architecture-wasm-iac.md`, `specs/production-viability-gap-analysis.md`, `specs/read-only-graph-view-contract.md`
**Companion artifacts:** `agentic-manifest.json` (machine-readable module map) · `docs/agent-onboarding.md` (coding rules for the build loop)

> This document condenses Phase 2 into one readable plan for the MVP build. It is the narrative sibling of `agentic-manifest.json`, which remains the executable source of truth for Prompt 3 subagents.

---

## 1. What we are building

Weavster is a **message-oriented integration platform**: it receives messages from many sources (files, HTTP, TCP/MLLP, databases, SMTP, web services), applies filters and transformations authored declaratively (YAML DSL) or as sandboxed WASM, routes them to one or more destinations, and provides durable storage, search, export, scheduling, alerting, administration, and automation — hardened for regulated data (HL7 v2, X12, NCPDP, DICOM, XML, JSON).

It is a **greenfield replacement** for the legacy integration engine, built to the non-negotiable constraints of a **single static binary**, **WASM-sandboxed user logic**, **local DX without Postgres**, and **100% code-defined configuration (IaC)**.

## 2. Confirmed stack (locked in Phase 2)

| Decision | Choice |
|---|---|
| Control-plane language | **Go** (`>=1.22`), single static binary |
| Module path / binary | `github.com/weavster-dev/weavster` / `weavster` |
| Web framework | `net/http` + **chi** |
| WASM host runtime | **wazero** (pure-Go, zero CGo) |
| Transform authoring | Declarative **YAML DSL** (default) + multi-language **WASI** (advanced) |
| YAML-DSL codegen | **Go + TinyGo → WASI** (pinned, reproducible toolchain) |
| Databases | **PostgreSQL** (prod) · **SQLite** (local DX) · **in-memory** (passthrough/buffered) |
| API | REST + OpenAPI 3.1 (JSON-first) |
| Config & IaC | YAML/JSON config-as-code + Terraform/OpenTofu/Pulumi samples |
| Docs | **MkDocs** human site + **`agent-docs/`** (OpenAPI, JSON Schemas, `llms.txt`) |
| Testing | Go `testing` + `weavster test` wrapper (JUnit XML / JSON) |

**Architecture principle:** hexagonal (ports & adapters). Every component depends only on a **Go interface (port)**, never another component's concrete type. This is what allows the monolith to be split per-component on Kubernetes later without a rewrite.

## 3. MVP scope — in vs out

**In scope (MVP):** single Go binary/container; Postgres + SQLite/in-memory via the `Store` port; TLS + mTLS; local auth (password policy, lockout, anti-enumeration); built-in RBAC permission set; YAML DSL (auto-compiled) + WASI transforms; internal scheduler with Postgres `SKIP LOCKED` job claiming; `weavster test`; Git-backed config-as-code + IaC samples; REST API + OpenAPI; **read-only** flow-topology web UI; Prometheus metrics + structured logs + events; and the **three critical gap closures** below.

**Explicitly out (Enterprise, port exists but implementation excluded):** SSO (OIDC/SAML), complex RBAC/ABAC (OPA/Cedar), immutable/SIEM audit, K8s horizontal scaling, Redis/NATS queue + leader election, distributed tracing + transform replay, multi-tenancy, DICOM service-class provider. *Leave the interface, stub the rest.*

## 4. Critical gaps folded into the MVP (from the viability analysis)

| Gap | MVP closure |
|---|---|
| **#1 State migration** | First-class `import legacy` command + ETL (extract → transform → load) with dry-run report and opt-in `--with-content`. |
| **#2 WASM module lifecycle** | Versioned, signed, rollbackable module registry (draft → promoted → active → superseded → retired). |
| **#5 Idempotency & retries** | Transactional outbox + deterministic `idempotency_key` on all side effects; bounded retries + dead-letter state. |
| **#4 HA floor (partial)** | Durable job claim + lease heartbeat + startup reconciler (crash-safety only; full HA is Enterprise). |
| **#6 Config plan/apply** | `config plan` (dry-run diff) / `config apply`, drift detection, JSON plan output. |
| **#7 Migrations** | Versioned, forward-only `Store` migration runner with pre-upgrade checkpoint. |

## 5. Build topology (20 modules)

Full detail (paths, files, dependencies, acceptance criteria, frameworks) lives in `agentic-manifest.json`. Condensed map:

| Module | Path | Responsibility |
|---|---|---|
| API Gateway | `internal/gateway` | REST + OpenAPI, authN/Z, audit, TLS/CSRF/security headers |
| Auth & Authorization | `internal/auth` | AuthProvider/Authorizer, password policy, lockout, MFA hook |
| Audit Log | `internal/audit` | AuditSink, PHI access logging |
| Scheduler | `internal/scheduler` | cron/interval + durable job queue + reconciler + lease |
| Executor | `internal/executor` | wazero runtime + resource limits + host functions |
| State Manager | `internal/state` | Store port (Postgres/SQLite/memory) + migrations + search/export |
| Adapters | `internal/adapters` | Source/Sink ports (file/http/tcp-MLLP/db/smtp/webservice/interflow/document) |
| Data-Type Codecs | `internal/codecs` | HL7 v2, X12, NCPDP, JSON, XML, delimited, raw |
| Transform Compiler | `internal/compiler` | YAML DSL → TinyGo → WASM + schema validation |
| Module Registry | `internal/registry` | WASM module version/sign/promote/rollback/GC *(glue, gap #2)* |
| Config-as-Code | `internal/config` | plan/apply/drift, JSON-Schema validation |
| Git Store | `internal/gitstore` | native Git-backed config (commit/push/pull/history/restore) |
| Legacy Import ETL | `internal/migrate` | legacy XML → YAML migration *(glue, gap #1)* |
| Outbox & Idempotency | `internal/outbox` | transactional outbox + idempotency keys + retry/dead-letter *(glue, gap #5)* |
| Alerts | `internal/alerts` | alert definitions + evaluation |
| Notifier | `internal/notify` | Notifier port (SMTP/webhook) |
| Secrets | `internal/secrets` | SecretProvider (local store + env) |
| Observability | `internal/observability` | Prometheus metrics + slog + OTel + events + stats |
| Topology Graph | `internal/topology` | read-only flow topology (MVP UI data contract) |
| CLI / Server Entry | `cmd/weavster` | composition root + CLI shell + `weavster test` |

## 6. Build sequence (Prompt 3 milestones)

Dependency-aware ordering; each phase is a mergeable, testable slice.

1. **P0 — Scaffold.** `go.mod`, CI (build+vet+test), `.gitignore`, `AGENTS.md`, MkDocs + `agent-docs/` skeleton.
2. **P1 — Foundational leaves** (no internal deps): `codecs`, `secrets`, `observability`, `gitstore`, `config` (parse + validate + schemas).
3. **P2 — Durable state.** `state` (Store port + migrations + search), then `outbox`.
4. **P3 — WASM layer.** `registry`, then `executor`, then `compiler`.
5. **P4 — Orchestration.** `scheduler`, then `adapters`.
6. **P5 — Control plane.** `auth`, `audit`, `notify`, `alerts`, `topology`, then `gateway`.
7. **P6 — Composition root.** `cmd/weavster` (server + CLI shell + `weavster test`).
8. **P7 — Migration + IaC + docs.** `migrate` (ETL), Terraform/OpenTofu/Pulumi samples, MkDocs content, `agent-docs/` (OpenAPI + schemas + `llms.txt`).

## 7. Definition of done

Per module (from `docs/agent-onboarding.md`): acceptance criteria implemented; `gofmt -l .` empty; `go vet ./...` clean; `golangci-lint run` clean; `go test -race ./<module>/...` green; no CGo; no second language; no Postgres required in tests; Enterprise items left as ports/stubs. Whole-system gate: `weavster test --format junit` passes the golden-path fixtures, and the binary cross-compiles (`linux/amd64`, `linux/arm64`, `darwin/arm64`) and builds to a distroless non-root container.

---

*Collated from Phase 2. If this plan and `agentic-manifest.json` ever conflict on a technical fact, the manifest wins for Prompt 3 subagents; the specs win for semantics; surface any disagreement rather than resolving it silently.*
