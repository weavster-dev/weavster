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
