# Target Architecture — WASM + IaC Blueprint

**Status:** Draft for review
**Phase:** 2 (Greenfield re-architecture)
**Inputs:** `specs/black-box-functional-spec.md` (clean-room requirements) + non-negotiable stack constraints.
**Naming note:** Step 3 of the task referenced a filename `target-architecture-blueprint.md`; the canonical deliverable (Step 6) is this file, `target-architecture-wasm-iac.md`.

---

## 1. Confirmed Stack Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Control plane language | **Go** | Single static binary for one-command installs; goroutine-native scheduler/executor; first-class embedded WASM (wazero); trivial cross-compile (linux/amd64, linux/arm64, darwin). |
| WASM host runtime | **wazero** | Pure-Go, zero CGo/deps, embeds directly into the single binary; WASI-complete; small attack surface; clean CPU/memory limits. |
| User transform authoring | **Declarative YAML DSL (default) + multi-language WASI (advanced)** | Non-programmer authors declare filters/transforms in YAML; the platform compiles them to WASM in the platform's preferred guest language. Advanced users author directly in Rust / Go+TinyGo / TypeScript (AssemblyScript/Javy) / C via WASI + Component Model. |
| YAML-DSL codegen target | **Go + TinyGo** (confirmed) | Same toolchain as the control plane; TinyGo → WASI runs on wazero; one pinned, reproducible toolchain for the auto-build path. |
| WASM sandboxing | wazero `ModuleConfig` limits + `InterruptOnTimeout` | Fuel (CPU) budget, max memory pages, wall-clock timeout, WASI stdio capture, no filesystem/network by default. |
| Production database | **PostgreSQL** | Named in MVP scope; the durable State Manager backend. |
| Local DX database | **SQLite / in-memory** | No Postgres required for local transform testing (non-negotiable constraint). |
| API protocol | **REST + OpenAPI 3.1** (JSON-first; XML where parity requires) | Machine-readable contract in `agent-docs/`. |
| Config & IaC | **YAML/JSON config-as-code** + **Terraform/OpenTofu/Pulumi** sample modules | Non-negotiable. |
| Docs | **MkDocs** site + **`agent-docs/`** (OpenAPI, JSON Schemas, `llms.txt`) | Non-negotiable. |

**Decision (confirmed):** the management console is a **read-only web UI** served by the single binary (no desktop application). Read-only preserves SDLC best practice — Git/CI stays the *only* mutation path; the UI never edits configuration. Primary MVP view: a **flow topology/connectivity graph** (node/edge, "Svelte Flow"-style) showing flows, sources, transforms, destinations, and runtime activity. Other read-only views (operations dashboard, message browser, events/logs) are deferred/TBD.

---

## 2. Non-Negotiable Constraints (restated)

1. **Installation** — one command (`curl | bash`, `brew install`, `apt-get install`, or a signed static binary). Zero heavy SDKs (Rust/Java/.NET) required on the end-user host.
2. **Transforms/filters** — all user business logic compiles to **WASM**; the host treats WASM modules as sandboxed plugins.
3. **Local DX** — running locally (testing transforms) must not require Postgres; use SQLite or in-memory state.
4. **Testing** — built-in `test` command; JUnit XML or JSON output; runs via CLI and natively in CI/CD (GitHub Actions/GitLab).
5. **IaC** — configuration is 100% code-defined (YAML/JSON); deployment/updates driven by Terraform/OpenTofu/Pulumi; sample modules provided.
6. **Modularity (monolith-first)** — MVP is a single binary / single OCI container; internal boundaries (API Gateway, Scheduler, Executor, State Manager) decoupled via Hexagonal/Ports & Adapters so they can be independently containerized and scaled on Kubernetes later.
7. **Documentation** — first-class: MkDocs/Docusaurus human site + `agent-docs/` (OpenAPI schemas, JSON Schemas for configs, `llms.txt`).

---

## 3. High-Level Architecture (Hexagonal, Monolith-First)

```
                        ┌──────────────────────────────────────────────────────────┐
                        │                     SINGLE BINARY / OCI                   │
                        │                                                          │
  [CLI] ─────HTTPS─────▶│  API GATEWAY (REST + OpenAPI, authN/Z, RBAC, audit)     │
  [Web UI] ───HTTPS────▶│        │                                                 │
  [CI/CD] ───HTTPS─────▶│        ▼                                                 │
                        │   SCHEDULER (cron + interval, durable job queue)         │
                        │        │  claims jobs (Postgres FOR UPDATE SKIP LOCKED)  │
                        │        ▼                                                 │
                        │   EXECUTOR (WASM runtime: wazero)                        │
                        │        │  sandboxed transforms, resource limits          │
                        │        ▼                                                 │
                        │   STATE MANAGER (Postgres │ SQLite │ in-memory)          │
                        │        ▲                                                 │
                        │   ADAPTERS (ports): file/http/tcp/db/queue/smtp/ws/…     │
                        └───────────┬──────────────────────────────────────────────┘
                                    │ each boundary is a Go interface (port)
                                    │ swappable per environment
              ┌─────────────────────┼─────────────────────────────┐
              ▼                     ▼                             ▼
        [PostgreSQL]         [Prometheus / OTel]            [External systems]
        (prod durable)       (metrics/traces)               (files, HTTP, HL7 peers…)
```

**Design principle:** every internal component depends only on a **port** (Go interface), never on another component's concrete type. Adapters implement ports. This is what enables future independent containerization (constraint #6) without a rewrite.

### 3.1 Component boundaries (ports)

| Port | Responsibility | MVP adapters | Enterprise adapters |
|---|---|---|---|
| `Source` / `Sink` | Message acquisition/delivery | file, http, tcp (MLLP), in-memory, database, smtp, web-service, document | message-queue (broker), medical-imaging (DICOM) |
| `Store` | Durable state | Postgres, SQLite, in-memory | sharded Postgres, object storage for blobs |
| `JobQueue` | Scheduler work claiming | Postgres `SKIP LOCKED` | Redis/NATS, dedicated queue |
| `TransformEngine` | Sandboxed user logic | wazero (embedded) | wazero (same, scaled) |
| `AuthProvider` | Identity/authN | local user store | OIDC/SAML SSO |
| `Authorizer` | authZ/RBAC | built-in permission set | policy engine (OPA/Cedar), ABAC |
| `AuditSink` | Audit log delivery | local event store | SIEM export, immutable store |
| `SecretProvider` | Credential material | local credential store, env | cloud KMS/Vault |
| `MetricsExporter` | Observability | Prometheus + OTel stdout/OTLP | managed OTel collector |
| `Notifier` | Alert delivery | email (SMTP), webhook | PagerDuty/Slack/Alertmanager |

---

## 4. WASM Execution Model (the core of the re-architecture)

### 4.1 Two authoring paths

**Path A — Declarative YAML (default, for non-programmers).** Users write a filter/transform as data:

```yaml
kind: Transform
name: normalize-patient-name
inputs: [message]
steps:
  - map: { from: "PID.5.1", to: "patient.lastName", type: string }
  - map: { from: "PID.5.2", to: "patient.firstName", type: string }
  - set: { field: "patient.fullName", expr: "{{patient.lastName}}, {{patient.firstName}}" }
  - filter: { when: "patient.lastName == ''", action: reject }
```

The platform's **transform compiler** transpiles this YAML into a WASM module (generated in **Go+TinyGo**, then compiled with a pinned, reproducible toolchain) and caches the artifact. YAML is the source of truth; the WASM module is a build artifact, never hand-edited.

**Path B — Multi-language WASI (advanced).** Users author a guest module directly in Rust, Go+TinyGo, TypeScript (AssemblyScript/Javy), or C, compiled to WASM against the platform's **guest SDK** (a WASI ABI + host-function imports). The SDK is a versioned contract.

### 4.2 Guest contract (host functions = the capability surface)

The WASM guest is **sandboxed**: no filesystem, no network, no clock by default. All capabilities are explicitly imported **host functions**, which are the replacement for the legacy scripted utility surface:

| Host function group | Capabilities (replacing legacy script API) |
|---|---|
| `parse` / `serialize` | Data-type codecs (HL7 v2, X12, EDI, NCPDP, DICOM, JSON, XML, delimited, raw) |
| `ack` | Generate acknowledgment responses |
| `route` | Inter-flow routing (by name/id) |
| `store` | Durable lookup/read of config map + dynamic lookups |
| `net` (opted-in) | Explicitly-granted HTTP/SMTP/database access for the script-style adapters |
| `crypto` / `hash` | Encryption, digest, UUID, date utilities |

Host functions are registered per-module at instantiation time based on the flow's declared capabilities (least privilege).

### 4.3 Resource limits (mandatory)

Every module instantiation sets: **fuel** (CPU instruction budget), **max memory pages**, **wall-clock deadline** (interrupt), and **WASI stdio capture**. Exceeding a limit aborts the module and produces a structured error carrying module name + version + input hash + limit type.

---

## 5. Feature → New Implementation Mapping

Every Black-Box behavior (§2 of the functional spec) is mapped to its new implementation. This is the Step 3 translation table.

| # | Black-box behavior | New implementation |
|---|---|---|
| 1 | Author flows (source + destinations + filters/transformers) | Flows defined as YAML in the config-as-code store; validated by JSON Schema; source/sink chosen from adapter ports. |
| 2 | Undeployed (draft) persistence | Flows stored in `Store` with a `state` column; draft = not yet deployed. |
| 3 | Rename/enable/disable + cross-flow deps | Flow CRUD in API Gateway; dependency graph persisted and resolved on import/export. |
| 4 | Script-based filter/transform + declarative steps | **YAML DSL (Path A) or WASI module (Path B)**, executed by the wazero `TransformEngine`; declarative steps (map/build/XSLT) compiled to WASM. |
| 5 | Destination-set filter | A standard host-function/`filter` step exposed to every transform that computes the allowed destination set. |
| 6 | Reusable utility functions | Guest SDK host functions (§4.2). |
| 7 | Response transformer + selector | A dedicated response-transform WASM stage + a response-selector stage, both sandboxed. |
| 8 | Route to all non-excluded destinations | Executor fan-out to `Sink` adapters; exclusion from the destination-set result. |
| 9 | Inter-flow routing | `route` host function → in-process dispatch to another flow's source. |
| 10 | Transmission modes | Per-sink transport options in flow YAML (e.g., MLLP framing for the TCP sink). |
| 11 | Polling interval | `Scheduler` cron/interval entries per source; durable in the job queue. |
| 12 | Cron-like schedules | Scheduler cron parser + per-source schedule in YAML. |
| 13 | Per-destination queuing + retry | `JobQueue` claims + per-(message,destination) attempt/error counters in `Store`. |
| 14 | Send-attempt / error tracking | `Store` columns on connector-message rows; indexed for the message-search filter surface. |
| 15 | Return to queued + reprocess later | Failed sends transition state to `queued`; queue worker retries with backoff. |
| 16 | Recover queue after restart | Durable queue in Postgres/SQLite survives restart; startup reconciler re-claims abandoned jobs. |
| 17 | Store message content per policy | `Store` backends: Postgres (durable), SQLite (local), in-memory (passthrough/buffered). |
| 18 | Message search surface | SQL/query layer over `Store` exposing all generic filters (id ranges, dates, status, attempts, content subtypes, metadata). |
| 19 | Export multiple forms + archive/compress/encrypt | `Store` export path producing raw/processed/transformed/encoded/response forms; archive/compress/encrypt in the export module. |
| 20 | Import messages (path/archive) | `import` path reverses export; restores content + metadata. |
| 21 | Reprocess (id/filter/prior-flow) | Executor re-entry: re-run through the flow; result stored as new content. |
| 22 | Remove messages + restart flows | `Store` delete paths + flow-restart side effect on clear-all. |
| 23 | Data pruning (age/size) | Background pruner worker (a Scheduler job) deleting by age/size; status endpoint. |
| 24 | Alerts (triggers/recipients/scope) | Alert rules in YAML; evaluated on error events; delivered via `Notifier` (SMTP/webhook). |
| 25 | Alert enable/disable/import/export/test | Alert CRUD in API Gateway; export/import in config-as-code; test = fire once. |
| 26 | User CRUD + password change | Local `AuthProvider` user store in `Store`. |
| 27 | Per-resource permissions | `Authorizer` with the built-in permission set (per resource category). |
| 28 | External auth hook + MFA hook | `AuthProvider` and MFA ports (interfaces); MVP ships local impl, Enterprise ships OIDC/SAML. |
| 29 | Export/import flows/config/alerts/scripts/snippets/map | Config-as-code export/import (YAML/JSON); Git-backed store is the canonical serialization. |
| 30 | Import with nodeploy/overwrite-map | Import options in the `import` command/API. |
| 31 | File-not-found / force conflicts | Explicit error codes + `--force` flag. |
| 32 | Scriptable shell (interactive + batch) | Go CLI; identical command surface (functional spec §3); `-s` script mode. |
| 33 | Programmatic client automation | REST API (+ generated client SDK from OpenAPI). |
| 34 | Git-style version control of artifacts | Config-as-code stored in a **Git-backed config store** (native; replaces the legacy bolt-on). Commit/push/pull/history/restore map to Git operations. |
| 35 | Event log (search/count/export) | Structured events in `Store`; search/count/export endpoints. |
| 36 | Per-flow statistics + reset/dump | In-memory counters flushed to `Store`; Prometheus gauge/counter mirrors; reset/dump endpoints. |
| 37 | Time-series stats + log viewer | Time-series table in `Store` + Prometheus; log tail endpoint. |
| 38 | System status info | `/system` endpoint reading runtime metadata. |
| 39 | Historical revisions | Git store history + content-at-revision endpoints. |
| 40 | Commit/push/pull/restore | Git store ops (remote-wins pull). |
| 41 | Password policy | `AuthProvider` enforcing min length/classes/expiration/grace/reuse. |
| 42 | Account lockout | `AuthProvider` strike counting + lockout window. |
| 43 | Anti-enumeration | Generic failure message toggle (default on). |
| 44 | HTTPS + TLS protocols/ciphers + credential store | Go TLS config; cert/key from file or `SecretProvider`; configurable protocols/ciphers. |
| 45 | Security headers + CSRF | API Gateway middleware (HSTS/CSP/X-Frame-Options/nosniff + CSRF marker header check). |
| 46 | (Implied) XXE-safe parsing | Data-type codecs disable external entities/DTD by construction. |
| 47 | (Implied) PHI/audit access logging | `AuditSink` writes protected-content access events; sensitive params excluded. |

**Key translations (the two named examples):**
- *"Dynamic SQL filters" (legacy scripted DB access)* → **New:** user writes a YAML filter or WASI function; the `TransformEngine` (wazero) invokes it with strict CPU/memory/time limits; DB access goes through the `store`/`net` host function with an explicitly granted capability.
- *"Scheduled reports" (legacy scheduled delivery)* → **New:** the internal `Scheduler` triggers a flow's polling source; the `Executor` runs the sandboxed transform; results persist to the `Store` (Postgres); metrics exposed via Prometheus.

---

## 6. Configuration-as-Code & IaC

- **Single source of truth:** all flows, alerts, snippets, scripts, maps, and settings are YAML/JSON under a versioned config root (Git-backed).
- **Schema enforcement:** every config artifact has a JSON Schema published in `agent-docs/schemas/`; the CLI/API validate on load and reject invalid configs.
- **Plan vs apply for config:** a built-in `config diff` / `config validate` / `apply --dry-run` produces a plan of changes (flows to add/update/remove, drift detection) *before* mutation — see the viability-gap analysis (gap #6).
- **Infrastructure:** sample Terraform/OpenTofu and Pulumi modules provision the VM/container, Postgres, TLS certs, and DNS. The platform config is applied *as data* by the same pipeline (GitOps: config push → plan → apply).
- **Update path:** `curl | bash` / `brew` / `apt` / signed binary; IaC pins the version; in-place binary swap + DB migration on startup.

### 6.1 Sample Terraform module (illustrative)

```hcl
module "weavster" {
  source          = "./modules/weavster"
  version         = "0.1.0"
  database_url    = module.postgres.url            # or "sqlite:///var/lib/weavster/db"
  tls_cert_arn    = aws_acm_certificate.cert.arn
  config_repo     = "git::https://example.com/flows.git"
  enable_mtls     = true
  metrics_enabled = true
}
```

---

## 7. Testing Framework (built-in `test` command)

- `weavster test [--filter NAME] [--format junit|json] [--output DIR]` runs a flow's transforms against fixture messages.
- **Local DX:** `test` runs against **SQLite or in-memory** state — no Postgres required (constraint #3).
- **Output:** JUnit XML (`--format junit`) for CI dashboards, or JSON for programmatic use. Non-zero exit on failure.
- **CI/CD:** sample GitHub Actions + GitLab CI workflows invoke `weavster test` and publish the JUnit report.

```yaml
# .github/workflows/transforms.yml (illustrative)
- uses: actions/checkout@v4
- run: curl -sSL https://install.weavster.dev | sh
- run: weavster test --format junit --output artifacts/
- uses: EnricoMi/publish-unit-test-result-action@v2
  with: { files: artifacts/*.xml }
```

---

## 8. Packaging, Deployment & Docs

- **Artifacts:** single static binary (linux/amd64, linux/arm64, darwin/arm64) + OCI container (distroless, non-root).
- **Install:** `curl | bash` installer; Homebrew tap; Debian/RPM repos. No Rust/Java/.NET runtime needed on the host.
- **Docs:** MkDocs human site (getting started, flow authoring, YAML DSL reference, guest SDK reference, operations runbook) + `agent-docs/` containing `openapi.yaml`, `schemas/*.json` (flow/config/transform JSON Schemas), and `llms.txt` (agent context index).

---

## 9. MVP vs Enterprise Split

### 9.1 MVP (in scope, this phase)

- Single Go binary / single OCI container on a simple VM.
- Postgres (production durable store) **and** SQLite/in-memory (local DX) via the `Store` port.
- **SSL/TLS** (HTTPS) and **mTLS** (client-certificate auth for the API and for adapter peers).
- Local user authentication with password policy, lockout, anti-enumeration.
- Built-in permission-based authorization (the resource-category permission set).
- YAML DSL transforms (auto-compiled) + WASI multi-lang transforms via wazero.
- Internal scheduler with Postgres `SKIP LOCKED` job claiming (single-node dedup).
- Built-in `test` command (JUnit XML/JSON).
- Git-backed config-as-code + Terraform/OpenTofu/Pulumi sample modules.
- REST API + OpenAPI 3.1; **read-only** web UI (flow topology/connectivity graph) served by the binary; CLI.
- Prometheus metrics + structured logs + events.
- **Critical-gap closures (folded into MVP per stakeholder decision):**
  - **Legacy data import/migration** — a first-class `import legacy` command + ETL adapter for the legacy export format (see gap #1).
  - **WASM module lifecycle** — a versioned, signed, rollbackable module registry (see gap #2).
  - **Idempotency & retries** — transactional outbox + deterministic idempotency keys on all external side effects (see gap #5).

### 9.2 Enterprise extension points (explicitly **out of MVP scope** — tagged exclusions)

These are commercial add-ons. Each has a **port (interface) in the MVP** so the hook exists without the implementation:

| Extension | MVP hook (interface exists) | Excluded implementation |
|---|---|---|
| **SSO (OIDC/SAML)** | `AuthProvider` port | External IdP integration, token exchange, group sync |
| **Complex RBAC / ABAC** | `Authorizer` port | Fine-grained roles, org/tenant scoping, policy engine (OPA/Cedar) |
| **Audit Logs (enterprise)** | `AuditSink` port | Immutable store, SIEM export, retention/legal-hold (PHI access audit is MVP) |
| **Horizontal scaling** | component ports (JobQueue/Store/Executor) | K8s deployments, sharded scheduler, Redis/NATS queue, distributed leader election, autoscaling |
| **User-logic observability** | executor span hooks + wazero resource limits | Distributed tracing (OTel → collector), transform replay debugging, fuel/memory dashboards (gap #3, deferred) |
| **Multi-tenancy** | (reserved in `Authorizer`/`Store`) | Tenant isolation, per-tenant quotas |
| **Managed medical-imaging (DICOM) adapter** | `Source`/`Sink` port | Full DICOM service-class provider (SCU/SCP) |

---

## 10. Resolved Decisions & Remaining Open Questions

### 10.1 Resolved (this phase)

1. **YAML-DSL codegen target** → **Go + TinyGo** (confirmed): same toolchain as the control plane; TinyGo compiles to WASI and runs on wazero; one pinned, reproducible toolchain for the auto-build path.
2. **Web UI** → **read-only** (confirmed): Git/CI is the only mutation path. Primary MVP view is a **flow topology/connectivity graph** (node/edge, "Svelte Flow"-style) showing flows, sources, transforms, destinations, and runtime activity. A full authoring UI is explicitly **out of scope**. Data contract: `specs/read-only-graph-view-contract.md`.

### 10.2 Remaining open questions

1. **Web UI read-only view set beyond the graph** — whether/when to add the operations dashboard, message browser, and events/logs views (deferred/TBD; the graph is the confirmed MVP surface).
2. **Blob storage** for large message content (Postgres `BYTEA` vs object storage) — recommended: Postgres for MVP, object-storage port for Enterprise.
3. **mTLS scope** — mandatory for the API by default vs opt-in per listener.
4. **Backward-compat import format** — whether the legacy XML export format is parsed natively by the `import` command (ties to viability gap #1).

---

*This blueprint satisfies Steps 2–4. The remaining document (`production-viability-gap-analysis.md`) identifies what is still missing to be a production-grade replacement and rates each gap.*
