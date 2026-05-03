# Plan: 20260502160602-sqlite-local-runtime-docs-cleanup

<!-- Metadata -->
<!-- Created: 2026-05-02T16:08:36Z -->
<!-- Commit: 6d593fe -->
<!-- Branch: cleanup-sqlite-local-runtime-docs -->
<!-- Repository: https://github.com/weavster-dev/weavster.git -->

## Overview

This plan cleans up the remaining local-runtime wording that still implies embedded PostgreSQL is part of the current local development path. Users and contributors should see SQLite as the local state backend, while existing configuration compatibility is preserved.

## Architecture & Design Decisions

The chosen direction is a compatibility-preserving cleanup. We will update generated starter configuration, documentation examples, and source-facing comments so they no longer describe embedded PostgreSQL or an active local service port as part of the SQLite local runtime.

The important trade-off is that this plan does not remove the existing local port config field. That field is still accepted by the config parser and covered by existing tests, so removing it would turn a wording cleanup into a public configuration compatibility change. See `research.md#alternatives-considered-and-rejected` for why removing the field and merging the old stale PR as-is were rejected.

## Component Breakdown

- **Starter project generation** owns the default configuration shown to new users. It should omit unused local runtime port guidance while keeping the generated project otherwise unchanged.
- **Project configuration model** owns compatibility with existing configuration files. It should keep parsing the local port field but describe it as compatibility-only rather than as embedded PostgreSQL configuration.
- **Documentation pages** own user-facing examples and status labels. They should avoid showing the local port as part of the recommended starter path and keep the SQLite local-state wording consistent.
- **Tests** own the compatibility proof. Existing config tests should continue to demonstrate that legacy local port values parse successfully.

## Data Structures & Interfaces

No new data structures or public interfaces are introduced. The existing project configuration shape remains compatible with `runtime.local.port`, but generated starter configuration no longer recommends that field.

## Implementation Detail

The implementation follows the existing documentation/status model from the current README and docs site. It removes recommended usage of the local port from generated and docs examples, updates comments to describe SQLite local state, and keeps the config parser behavior unchanged.

No new abstraction is introduced. This is a narrow text and scaffold cleanup around the existing local runtime configuration surface.

## Dependencies

- **Existing config parser**: Provides backward compatibility for legacy local port values and does not need behavior changes.
- **Existing CLI init command**: Provides the generated starter project and needs a small template update.
- **Existing docs baseline**: Provides current/partial/config-only status language and needs small alignment edits.
- **Closed PR #36**: Provides the motivating stale cleanup, but its broad docs changes are not reused because current main has superseded them.

## Testing Approach

Testing focuses on regression and compatibility. Existing config tests should continue to prove legacy local port parsing works, while CLI/docs checks confirm the recommended path no longer presents the local port or embedded PostgreSQL as local runtime setup.

The load-bearing assertions are that no unmarked embedded PostgreSQL local-runtime claims remain, generated starter config no longer contains the local port, and the workspace still passes the normal Rust and docs verification checks.

## Milestones & Phases

### Milestone 1: Local runtime wording matches SQLite behavior

**What changes**: New users and contributors no longer encounter stale embedded-PostgreSQL setup language in the local runtime path. Starter configuration and reference docs present SQLite-backed local state accurately while existing projects remain compatible.

#### - [x] Phase 1.1: Clean up SQLite local runtime wording

This phase removes the remaining stale embedded-PostgreSQL wording from source-facing comments, starter config, and docs examples. It keeps legacy local port parsing intact so existing configuration files are not broken by a documentation cleanup.

*Technical detail:* [context.md#phase-11-clean-up-sqlite-local-runtime-wording](./context.md#phase-11-clean-up-sqlite-local-runtime-wording)

**Acceptance criteria**:

- [x] Starter projects no longer include a local runtime port value by default.
- [x] Documentation examples no longer present the local port as part of the recommended local runtime configuration.
- [x] Source-facing comments describe SQLite-backed local state and compatibility parsing rather than embedded PostgreSQL.
- [x] Legacy configuration containing a local runtime port still parses successfully.
- [x] Verification passes for formatting, tests, clippy, docs, and stale-claim scans.

## Open Questions

There are no open questions. The cleanup scope and compatibility boundary are fixed.

## Out of Scope

- Removing the `runtime.local.port` field from the configuration API.
- Changing SQLite or Postgres state-store selection.
- Reworking remote runtime support.
- Resurrecting the broad stale documentation edits from closed PR #36.

## Changelog

### 2026-05-02 — Phase 1.1: Clean up SQLite local runtime wording

**What was done**: Removed `runtime.local.port` from generated starter configuration and the docs example, updated source-facing local runtime comments to describe SQLite-backed state, and renamed the internal default helper away from Postgres terminology. Added a CLI integration assertion that checks the raw generated YAML omits the local port while leaving compatibility parsing intact.

**Deviations**: None.

**Files changed**:
- `crates/weavster-cli/src/commands/init.rs`
- `crates/weavster-cli/tests/cli_test.rs`
- `crates/weavster-core/src/config.rs`
- `docs/docs/configuration/project.md`
- `.spektacular/specs/20260502160602-sqlite-local-runtime-docs-cleanup.md`
- `.spektacular/plans/20260502160602-sqlite-local-runtime-docs-cleanup/plan.md`
- `.spektacular/plans/20260502160602-sqlite-local-runtime-docs-cleanup/context.md`
- `.spektacular/plans/20260502160602-sqlite-local-runtime-docs-cleanup/research.md`

**Discoveries**: The remaining `port: 5433` occurrences after cleanup are compatibility/test values, not generated or recommended configuration. The README and first-flow docs still contain negated wording that explicitly says local state uses SQLite, not embedded PostgreSQL.
