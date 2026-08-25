# Changelog

All notable changes to this project are documented here, following
[Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

### Added

- Seed greenfield Go repository: Phase 2 specs, build manifest, MVP project plan, agent onboarding, CI, MkDocs + agent-docs skeletons.
- P0 scaffold: `cmd/weavster` composition-root stub, stub packages for all 20 modules, `.golangci.yml`, and agent-state gitignore entries (`go build ./...`, `go test ./...`, `go vet ./...`, `golangci-lint run` all green).
- P1 foundational leaves: `codecs` (HL7 v2 + X12 997 ACK, JSON, XXE-safe XML, delimited, NCPDP, raw, DICOM enterprise stub, coverage matrix), `secrets` (SecretProvider local/env + KMS interface), `observability` (Prometheus + OTel stdout/OTLP, slog, events, per-flow stats, time series, system status), `gitstore` (go-git commit/push/pull/history/diff/restore), and `config` (YAML/JSON parse, JSON-Schema validate, plan/apply/drift).
- P2 durable state: `state` (Store port with SQLite/Postgres/in-memory backends, migrations runner, search by id/date/status/attempts/metadata with pagination+sort, export/import with gzip + AES-GCM encryption, per-destination attempt tracking) and `outbox` (transactional outbox, deterministic idempotency keys, status-check-first ambiguous retry, bounded backoff, dead-letter + requeue).
- P3 WASM layer: `registry` (content-addressed SHA-256, Ed25519-signed, draft→active lifecycle, rollback, signature verification, GC) and `executor` (wazero TransformEngine with capability-scoped host functions, stdio capture, and fuel/memory/time limits producing structured errors; fuel enforced as wall-clock proxy — wazero has no instruction-count API) and `compiler` (YAML DSL → AST → Go+TinyGo source codegen + JSON-Schema validation + pinned-TinyGo build step).
- P4 orchestration: `scheduler` (JobQueue port with durable claim/heartbeat/lease/reconcile over Postgres SKIP LOCKED + SQLite equivalent, in-memory queue, interval/cron schedules) and `adapters` (Source/Sink ports: file, http, tcp MLLP, in-memory interflow, database, smtp, web-service, document; broker/DICOM enterprise stubs).
- P5 control plane: `auth` (AuthProvider/Authorizer ports, Argon2id hashing, password policy, lockout+decay, anti-enumeration, external auth + MFA hooks), `audit` (AuditSink local event store + PHI access logging with sensitive-parameter redaction), `notify` (SMTP + webhook), `alerts` (definitions, evaluation, delivery, enable/disable, import/export, test), `topology` (read-only overview + flow-internal graphs per the data contract), and `gateway` (chi router, OpenAPI 3.1 serving, CSRF marker, security headers, TRACE/TRACK blocking, configurable TLS).
- P6 composition root: `cmd/weavster` (server wiring all ports/adapters, scriptable CLI shell with batch `-s` mode and §3.3 exit codes, startup flags `-a/-u/-p/-s/-v/-c/-h/-d`, `weavster test --filter/--format/--output` with junit/json output, privileged-run guard; cross-compiles to linux/amd64, linux/arm64, darwin/arm64 with zero CGo).
- P7 migration + IaC + docs: `migrate` (legacy XML import ETL — extract/transform/load with dry-run report and a versioned legacy→YAML mapping table), Terraform + Pulumi sample modules (`iac/`), multi-stage distroless `Dockerfile`, generated `agent-docs/schemas/*.json`, full `agent-docs/openapi.yaml`, and updated README/MkDocs/llms.txt.
- `CONTRIBUTING.md` development guide (setup, build, gates, conventions, and PR process).
- Coverage gate: octocov (`.octocov.yml`, `acceptable: 70%`) wired into a `.github/workflows/coverage-gate.yml` CI job that runs `go test -coverprofile` and fails below threshold.
- Coverage uplift: added targeted unit tests across `adapters`, `alerts`, `compiler`, `executor`, `gitstore`, `notify`, `scheduler`, `state`, and `cmd/weavster` to raise overall statement coverage from 79.2% to 82.9%, satisfying the raised 80% gate.
- GitHub issue templates: bug report + feature request forms and blank-issue config (`.github/ISSUE_TEMPLATE/`).
- E2E tests (`test/e2e/`): real-HTTP tests of the gateway surface (OpenAPI spec, `/system`, security headers, TRACE/TRACK blocking, CSRF marker enforcement).
- `LICENSE`: Business Source License 1.1 (Licensor Weavster Dev, Change License MPL 2.0), brought forward from the weavster-old-v2 archive with the copyright year updated to 2026.
- Gateway coverage: unit test for the `POST /api/v1/flows` 503 branch when the Flows service is not wired (completes `handleFlowsCreate` coverage).

### Fixed

- `weavster test --format junit`: exclude the internal `passed` flag from JUnit XML so output is valid `<testcase name=.../>` elements.
- MkDocs: complete `mkdocs.yml` (site_url, full nav, exclude internal kickoff doc, lenient link validation), add `requirements.txt`, and a `docs.yml` workflow that deploys to GitHub Pages via `mkdocs gh-deploy` on the `gh-pages` branch.
