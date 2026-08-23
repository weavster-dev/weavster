# Changelog

All notable changes to this project are documented here, following
[Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

### Added

- Seed greenfield Go repository: Phase 2 specs, build manifest, MVP project plan, agent onboarding, CI, MkDocs + agent-docs skeletons.
- P0 scaffold: `cmd/weavster` composition-root stub, stub packages for all 20 modules, `.golangci.yml`, and agent-state gitignore entries (`go build ./...`, `go test ./...`, `go vet ./...`, `golangci-lint run` all green).
- P1 foundational leaves: `codecs` (HL7 v2 + X12 997 ACK, JSON, XXE-safe XML, delimited, NCPDP, raw, DICOM enterprise stub, coverage matrix), `secrets` (SecretProvider local/env + KMS interface), `observability` (Prometheus + OTel stdout/OTLP, slog, events, per-flow stats, time series, system status), `gitstore` (go-git commit/push/pull/history/diff/restore), and `config` (YAML/JSON parse, JSON-Schema validate, plan/apply/drift).
