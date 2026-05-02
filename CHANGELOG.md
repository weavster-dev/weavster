# Changelog

All notable changes to the Weavster project will be documented in this file.

This project follows a Spektacular-driven documentation workflow where approved specs and plans contribute to this master file.

## 20260502165842-workflow-quality-spektacular-alignment

Pull request quality checks now recognize the current Spektacular documentation workflow. Code changes can satisfy the release-note gate with a root changelog update or Spektacular spec/plan artifacts, and contributors no longer receive instructions for retired workflow changelog or Gemini autofix processes. Same-repository PRs can use a maintainer-applied `codex-autofix` label to generate a root changelog entry with Codex when release-note evidence is missing.

## 20260428024252-docs-current-vs-planned-alignment

The README and docs site now clearly separate what Weavster supports today from partial, config-only, placeholder, and planned functionality. New users get a source-based install path, a generated file-flow quick start that matches the current CLI, and explicit limits around non-file connectors, local SQLite state, package signing, and placeholder commands. The documentation now gives contributors a single status baseline for future README, docs, and test-health updates.

## [Unreleased]

### Documentation & Cleanup
- **README Cleanup:** Aligned terminology with the codebase (e.g., `rename` -> `map`), updated the `Transforms` table, and clarified connector implementation status.
- **Roadmap:** Added a "Coming Soon" section for the MRK (Mapping, Routing & Keys) feature.
- **Docker Removal:** Removed all Docker-related files and descriptions to simplify the initial project foundation.
