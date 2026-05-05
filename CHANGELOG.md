# Changelog

All notable changes to the Weavster project will be documented in this file.

This project follows a Spektacular-driven documentation workflow where approved specs and plans contribute to this master file.

## 20260504125300-runtime-docs-alignment-followup

Removed ignored `weavster run --once` and `weavster run --flow` options from the CLI and updated README/docs to stop teaching those unsupported modes. The test command now honors the global config path for project discovery and relative fixtures, and docs now label current runtime limits around SQLite state path selection, conditional outputs, lookup artifacts, and generated transform chaining more precisely.

## 20260503150000-codegen-string-allocs

- Performance: optimize string allocations in codegen. The Rust code generator now uses `std::fmt::Write` and `write!` directly on the target `String` buffers, reducing temporary allocations in `crates/weavster-codegen/src/generator.rs` (including the coalesce transform).
- This aligns with the PR title "⚡ [performance] optimize string allocations in codegen".

## 20260502160602-sqlite-local-runtime-docs-cleanup

Starter projects and configuration documentation now present SQLite-backed local state without recommending an unused local runtime port. Existing projects that still include the legacy local port remain compatible, but new generated configuration no longer implies embedded PostgreSQL setup. This replaces the valid part of the old embedded-Postgres cleanup with a current-main scoped change.
## 20260503134624-pin-codex-action-nano-model

Maintainer-triggered changelog autofix jobs now explicitly use the GPT-5 nano model instead of relying on the Codex CLI default. This makes generated changelog updates more predictable and cost-efficient while preserving the existing pull request quality workflow and maintainer controls.

## 20260502165842-workflow-quality-spektacular-alignment

Pull request quality checks now recognize the current Spektacular documentation workflow. Code changes can satisfy the release-note gate with a root changelog update or Spektacular spec/plan artifacts, and contributors no longer receive instructions for retired workflow changelog or Gemini autofix processes. Same-repository PRs can use a maintainer-applied `codex-autofix` label to generate a root changelog entry with Codex when release-note evidence is missing.

## 20260428024252-docs-current-vs-planned-alignment

The README and docs site now clearly separate what Weavster supports today from partial, config-only, placeholder, and planned functionality. New users get a source-based install path, a generated file-flow quick start that matches the current CLI, and explicit limits around non-file connectors, local SQLite state, package signing, and placeholder commands. The documentation now gives contributors a single status baseline for future README, docs, and test-health updates.

## [Unreleased]

### Documentation & Cleanup
- **README Cleanup:** Aligned terminology with the codebase (e.g., `rename` -> `map`), updated the `Transforms` table, and clarified connector implementation status.
- **Roadmap:** Added a "Coming Soon" section for the MRK (Mapping, Routing & Keys) feature.
- **Docker Removal:** Removed all Docker-related files and descriptions to simplify the initial project foundation.
