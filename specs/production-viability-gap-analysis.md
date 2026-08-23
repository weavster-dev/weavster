# Production Viability Gap Analysis

**Status:** Draft for review
**Phase:** 2 (Greenfield re-architecture)
**Purpose:** Analyze the gap between (a) the Black-Box Functional Spec + new stack constraints and (b) a **production-grade replacement** for the three reference tools. Each gap is rated **Critical / High / Medium**, with a proposed solution and an MVP-vs-Enterprise disposition.

> **Decision (confirmed with stakeholder):** the **3 Critical gaps are folded into the MVP**; the **2 High gaps are deferred to Phase 3 (Enterprise)**. Dispositions below reflect this final split.

**Rating definitions:**
- **Critical** — blocks go-live or data migration for a real paying customer; no acceptable workaround.
- **High** — degrades production safety/operability materially; workaround exists but is costly.
- **Medium** — should be addressed before scale, but does not block initial go-live.

---

## Gap 1 — State Migration & Data Seeding

**Severity: Critical**

**What is missing.** The Black-Box spec defines import/export of *this system's own* artifacts (flows, alerts, snippets, config map, messages), but there is **no defined path for a customer to move their existing legacy data** — flows, code snippets, global scripts, users, stored message history, and configuration — into the new schema. No `import` adapter for the legacy export format exists; the new YAML config format and the legacy XML export format are not mapped.

**Why it matters.** Without this, a "replacement" cannot replace anything: every existing deployment would start from zero, losing years of integration configuration and message history. This is the single most common reason greenfield rewrites fail to be adopted.

**Proposed solution.**
1. Ship a first-class **`import legacy`** command (and API endpoint) that consumes the legacy XML/archive export format and maps it to the new config-as-code YAML and schema.
2. Implement it as an **ETL pipeline** with three explicit phases and a written mapping table: *extract* (parse legacy export archives: flows, snippets, scripts, users, config map), *transform* (legacy constructs → YAML DSL; legacy scripted filters → YAML DSL where expressible, else a WASI module stub flagged for human review), *load* (seed `Store`, then validate against JSON Schemas).
3. **Message history** is treated separately: import **metadata + references** by default, with an opt-in `--with-content` flag for full content migration (content is the high-volume, high-cost part).
4. Provide a **dry-run report** (counts + list of constructs that could not be auto-translated) before any write.
5. Make the legacy→YAML mapper a versioned, separately-tested component (its own transform fixtures) so the migration itself is testable via the built-in `test` command.

**Disposition:** **MVP** — must ship with the first release; no customer can adopt without it.

---

## Gap 2 — WASM Module Lifecycle Management

**Severity: Critical**

**What is missing.** The architecture executes WASM transforms, but does not yet define how a module is **uploaded, versioned, promoted, rolled back, signed, or garbage-collected**. "Just local files" is not production-viable: there is no registry, no immutable versioning, and no rollback story.

**Why it matters.** A transform is executable business logic with real side effects. Shipping it without versioning/rollback means a bad transform is unrecoverable and unauditable — the same operational risk the legacy system's version-control bolt-on tried to solve.

**Proposed solution.**
1. **Module registry** backed by the same Git/artifact store as config-as-code (content-addressed, e.g. SHA-256 of the `.wasm`). Each module is `{name, version, digest, source (YAML or guest source), signature, created-by}`.
2. **Lifecycle states:** `draft → promoted → active → superseded → retired`; only `active` modules are instantiable. Promotion is an explicit, audited action.
3. **Rollback = re-promote** a prior version (an atomic pointer move in the registry, not a rebuild).
4. **Signing:** modules are signed (cosign-style keyless or a platform signing key); the executor verifies signature before instantiation (ties to supply-chain security).
5. **Garbage collection:** `retired` modules with zero active references are prunable; a `module ls` / `module history` / `module rollback` command surfaces the lifecycle.
6. The YAML DSL path makes this cheap: the source is the YAML, the compiled `.wasm` is a cache artifact that can always be **rebuilt reproducibly** — so the registry can store *source* as the canonical object and treat binaries as derived.

**Disposition:** **MVP** — at minimum versioned + signed + rollbackable; full multi-tenant registry is Enterprise.

---

## Gap 3 — Observability for User Logic (tracing + debugging of WASM)

**Severity: High**

**What is missing.** Logs and metrics exist at the platform level, but there is **no trace correlation between an incoming request/message and the specific WASM instance (module name + version) that handled it**, and no defined debugging story for panics, timeouts, or memory exhaustion inside user transforms.

**Why it matters.** When a transform misbehaves (panic, infinite loop, memory leak), the operator cannot answer "which message, which module version, which input, and why?" without correlation. Healthcare integrations are auditable; a black-box transform failure is unacceptable.

**Proposed solution.**
1. **OpenTelemetry trace propagation into WASM:** inject a `trace_id`/`span_id` into the guest via a reserved host function or WASI-visible context; the executor records a span per transform with attributes `module`, `version`, `flow_id`, `destination`, `input_hash`.
2. **Structured failure record:** on panic/timeout/limit-exceeded, emit a structured error carrying `module`, `version`, `input_hash`, `limit_type`, `stdout/stderr tail`. The `input_hash` links the failure to the exact message (which is already persisted with content).
3. **Deterministic replay:** a `transform debug` subcommand re-runs a failed input through a *specific* module version with the same limits, enabling reproduction.
4. **Resource telemetry:** per-instantiation fuel consumed / memory used / wall-clock, exported as Prometheus histograms so operators can see *which* transform is hot before it OOMs.
5. Capture WASM stdout/stderr into structured logs keyed by trace_id (wazero already provides stdio capture; bind it to the logging layer).

**Disposition:** **Phase 3 (Enterprise)** — full trace correlation, replay debugging, and fuel/memory telemetry are deferred. MVP retains only a *minimal* structured panic/timeout error (`module` + `version` + `input_hash`) because wazero's resource limits make that nearly free and a silent black-box panic is otherwise undebuggable.

---

## Gap 4 — High Availability for the Scheduler (duplicate-execution prevention)

**Severity: High**

**What is missing.** The MVP runs as a single binary. If the machine dies mid-job, or if two instances are ever run against one database (e.g., a misconfigured replica, or an operator standing up a second node), there is **no leader election and no distributed job-claiming** to prevent duplicate executions. The spec says it *can* split later, but the split boundary (JobQueue/Store ports) is not yet enforced with a concrete claim protocol.

**Why it matters.** Duplicate execution of a transform that has side effects (send a message, bill a claim, write to a downstream system) is a correctness and compliance hazard. Even in single-node MVP, an unclean crash must not cause a half-processed message to be silently re-run without acknowledgment.

**Proposed solution.**
1. **MVP (must):** implement the durable job queue on **Postgres `FOR UPDATE SKIP LOCKED`** (or SQLite equivalent for local DX) so that *any* executor claiming a job takes an atomic, visible lock. Single-node crashes are recovered by the startup reconciler that re-claims jobs whose leases have expired — **with a monotonic job ID and a `claimed_by`/`lease_until` heartbeat**, so a stale node cannot double-claim.
2. **Idempotency guard** (see Gap 5) makes even an accidental double-claim harmless.
3. **Enterprise (defer):** distributed leader election (Postgres advisory-lock leader or etcd) + a dedicated queue (Redis/NATS Streams) + K8s autoscaling of the executor tier.

**Disposition:** **Phase 3 (Enterprise)** for multi-node HA (leader election, dedicated queue, K8s scaling). A minimal single-node crash-safety primitive (durable job claim + lease heartbeat + startup reconcile) is folded into the MVP as a *prerequisite* of Gap 5's outbox/idempotency contract — it is the safety floor, not the HA feature.

---

## Gap 5 — Idempotency & Retries (duplicate side-effects)

**Severity: Critical**

**What is missing.** The spec defines retry/queuing and send-attempt tracking, but **not the idempotency contract**: if a transform *succeeds* but the network/DB write that records success *fails*, the retry can re-execute the transform and duplicate a side effect (double-send, double-bill). The legacy system had only subtle, partial protection here.

**Why it matters.** In regulated messaging (HL7/X12/NCPDP), duplicate delivery is worse than a visible failure — it can double-charge a payer or double-dispense a result. The new system must not inherit the legacy system's ambiguity.

**Proposed solution.**
1. **Outbox pattern:** persist the *intent to deliver* and the *result* transactionally in `Store` **before** acknowledging to the source. The flow is: receive → persist → transform (WASM) → persist result → deliver → mark delivered. A crash at any point re-enters the *same* job with the *same* message ID.
2. **Idempotency keys:** every external side effect carries a deterministic `idempotency_key` derived from `(message_id, destination, attempt)`. Sinks that support it (HTTP headers, SMTP, database upsert) send the key so the downstream can dedupe. Sinks that do not (raw TCP MLLP) are flagged **at-least-once** and documented as such.
3. **Exactly-once where protocol allows, at-least-once + dedupe elsewhere:** the platform records the outcome and, on retry after an ambiguous result, prefers **"check status first, don't blindly re-send."**
4. **Explicit retry policy:** bounded retries with backoff, a dead-letter state, and a `deadletter` admin surface — never silent infinite retry.

**Disposition:** **MVP** — outbox + idempotency keys are required for any data-integrity claim; this is not a feature to defer.

---

## Gap 6 — CI/CD Integration for Configuration (plan vs apply diff)

**Severity: Medium**

**What is missing.** IaC is defined (Terraform/OpenTofu/Pulumi for infrastructure), but there is **no built-in `plan` vs `apply` diff mechanism for the platform configuration itself** — the Git-backed config-as-code can be applied, but there is no native dry-run that shows *what will change* (which flows added/updated/removed, drift detection) before mutating state.

**Why it matters.** Operators and auditors need a preview of config changes and a record of drift, mirroring the Terraform workflow. Without it, a config push is a blind mutation and drift goes undetected.

**Proposed solution.**
1. Built-in **`config plan`** (dry-run diff against live state) and **`config apply`** (mutate) commands, plus `--diff` output and a machine-readable JSON plan. This mirrors and complements external Terraform (Terraform plans *infra*; `config plan` plans *the platform data*).
2. **Drift detection:** periodic or on-demand comparison of live state vs the Git source of truth, with a report of out-of-band changes.
3. **CI/CD wiring:** sample GitHub Actions/GitLab workflows that run `config plan` on pull requests (comment the plan) and `config apply` on merge — the same GitOps loop as Terraform.
4. Store applied plans in the audit log so "what changed and who approved" is reconstructable.

**Disposition:** **MVP** (the `config plan`/`apply` primitives are small); full drift-remediation automation is **Enterprise**.

---

## Additional Gaps (beyond the six mandated)

| # | Gap | Severity | Note / proposed direction |
|---|---|---|---|
| 7 | **Platform self-upgrade & rollback** — the binary is updatable via IaC, but there is no defined schema-migration discipline for `Store` between versions (the legacy system had a versioned migration chain; the new one needs an equivalent, tested migration runner). | High | Ship a versioned, forward-only migration runner (like the legacy delta chain, but in `Store` migrations) with a pre-upgrade backup checkpoint. MVP. |
| 8 | **Secret management** — database passwords, SMTP creds, TLS keys currently imply files/env; no KMS/Vault story for production rotation. | Medium | `SecretProvider` port exists; MVP = env/`/run/secrets` + local credential store; Enterprise = cloud KMS/Vault with rotation. |
| 9 | **Backup/restore of Postgres** — no defined backup/DR runbook for the durable store (distinct from message export). | High | IaC-managed Postgres with automated snapshots/PITR in the sample modules + a documented restore procedure. |
| 10 | **License/entitlement gating** — the Enterprise features are tagged exclusions, but there is no entitlement mechanism to enable them cleanly later. | Medium | A version/entitlement flag surfaced in `system info`; feature flags gate Enterprise ports. |
| 11 | **Message-content volume / blobs** — storing full HL7/DICOM content as rows will not scale; need an object-store port for large payloads. | Medium | `Store` blob port; MVP = Postgres `BYTEA`/SQLite, Enterprise = object storage (S3/GCS) with content-addressed keys. |
| 12 | **Data-type codec completeness & licensing** — full HL7 v2 (all versions), X12, NCPDP, DICOM codecs are large and some may have licensing constraints; the MVP must state exactly which versions/segments are supported. | Medium | Explicit codec coverage matrix in docs; DICOM adapter flagged Enterprise (§9.2) if a licensed library is required. |

---

## Summary

| Severity | Gaps |
|---|---|
| **Critical** | 1 State migration & data seeding · 2 WASM module lifecycle · 5 Idempotency & retries |
| **High** | 3 Observability for user logic (→ Phase 3) · 4 HA scheduler (→ Phase 3) · 7 self-upgrade/migrations · 9 Postgres backup/DR |
| **Medium** | 6 config plan/apply · 8 secret management · 10 entitlement gating · 11 blob storage · 12 codec completeness |

**Net assessment.** The clean-room functional spec plus the imposed stack constraints cover the *what* (behavior) and the *how* (Go + wazero + WASM + IaC). What remains to be production-viable is concentrated in **three critical gaps** — data migration, WASM module governance, and an explicit idempotency/outbox contract — which are now **in MVP scope**. The **two high gaps** (scheduler HA, user-logic observability) are sequenced first on the **Phase 3 (Enterprise)** roadmap, where their interfaces (JobQueue/Store ports; executor span hooks) already exist.
