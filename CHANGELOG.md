# Changelog

All notable changes to the Weavster project will be documented in this file.

This project follows a Spektacular-driven documentation workflow where approved specs and plans contribute to this master file.

## 20260502160602-sqlite-local-runtime-docs-cleanup

Starter projects and configuration documentation now present SQLite-backed local state without recommending an unused local runtime port. Existing projects that still include the legacy local port remain compatible, but new generated configuration no longer implies embedded PostgreSQL setup. This replaces the valid part of the old embedded-Postgres cleanup with a current-main scoped change.

## 20260428024252-docs-current-vs-planned-alignment

The README and docs site now clearly separate what Weavster supports today from partial, config-only, placeholder, and planned functionality. New users get a source-based install path, a generated file-flow quick start that matches the current CLI, and explicit limits around non-file connectors, local SQLite state, package signing, and placeholder commands. The documentation now gives contributors a single status baseline for future README, docs, and test-health updates.

## [Unreleased]

### Documentation & Cleanup
- **README Cleanup:** Aligned terminology with the codebase (e.g., `rename` -> `map`), updated the `Transforms` table, and clarified connector implementation status.
- **Roadmap:** Added a "Coming Soon" section for the MRK (Mapping, Routing & Keys) feature.
- **Docker Removal:** Removed all Docker-related files and descriptions to simplify the initial project foundation.
