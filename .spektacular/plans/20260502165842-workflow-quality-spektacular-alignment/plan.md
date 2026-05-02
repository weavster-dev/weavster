# Plan: 20260502165842-workflow-quality-spektacular-alignment

<!-- Metadata -->
<!-- Created: 2026-05-02T17:00:26Z -->
<!-- Commit: 6d593fe -->
<!-- Branch: fix-workflow-quality-spektacular -->
<!-- Repository: https://github.com/weavster-dev/weavster.git -->

## Overview

This plan updates pull request quality checks so they align with the current Spektacular documentation workflow instead of the retired Gemini workflow changelog process. Code PRs should no longer fail because they lack obsolete `wf-changelog.md` artifacts, while contributors still get a clear release-note requirement and maintainers can request a Codex-generated root changelog entry.

## Architecture & Design Decisions

The chosen direction is to keep the workflow and key job names in place but replace the obsolete artifact detection and messaging. This preserves the visible PR check surface while changing the gate to recognize current artifacts: root changelog edits and Spektacular specs/plans.

The plan deliberately replaces the Gemini autofix path with Codex and scopes it to same-repository PRs. Codex may update only the root changelog; Spektacular specs and plans remain human/agent workflow artifacts, not CI-generated files. See `research.md#alternatives-considered-and-rejected` for why the plan does not delete the whole workflow or update release publishing automation in this PR.

## Component Breakdown

- **Pull request quality workflow** owns the additional release-note gate for code changes. It should detect Rust code changes, inspect current documentation artifacts, and produce actionable messages.
- **Spektacular artifacts** provide the current per-change planning and changelog evidence for substantial work. The workflow should count those artifacts as satisfying the code-change documentation requirement.
- **Root changelog** provides the user-facing release-note summary. The workflow should count root changelog updates as satisfying the requirement.
- **Codex autofix path** provides a maintainer-triggered fallback for same-repository PRs that are missing release-note evidence. It should modify only the root changelog and commit that change back to the PR branch.

## Data Structures & Interfaces

No application data structures or public interfaces change. The workflow contract changes from "code changes require `wf-changelog.md`" to "code changes require either a root changelog update or Spektacular spec/plan artifacts."

## Implementation Detail

The workflow keeps `Check Workflow Changelog` and `Fail if Missing` as job names, but updates the check script and failure message to current terminology. The old Gemini auto-generation path is replaced by `openai/codex-action@v1` behind a `codex-autofix` label and same-repository guard.

The check remains intentionally lightweight. It uses changed-file detection for Rust code and a git diff against the PR base to detect `CHANGELOG.md`, `.spektacular/specs/**`, or `.spektacular/plans/**` changes.

## Dependencies

- **GitHub Actions**: Runs the pull request workflow and needs valid YAML only.
- **OpenAI Codex Action**: Provides maintainer-triggered changelog generation and requires an `OPENAI_API_KEY` secret.
- **tj-actions/changed-files**: Already used by the workflow to detect code changes; no version change required.
- **Spektacular artifacts**: Current repository workflow for specs/plans; no behavior change required.

## Testing Approach

Testing focuses on workflow validity and deterministic shell logic. The workflow YAML should parse, the updated workflow should no longer contain Gemini or `wf-changelog.md` references, and a local script-equivalent check should demonstrate that code changes with Spektacular or root changelog artifacts do not set `needs_changelog=true`. Static review should also confirm the Codex autofix path is same-repository only and commits only `CHANGELOG.md`.

## Milestones & Phases

### Milestone 1: PR quality checks match Spektacular

**What changes**: Code pull requests are evaluated against the repository's current documentation workflow. Contributors no longer see instructions for retired Gemini labels or old workflow changelog files.

#### - [x] Phase 1.1: Replace retired changelog gate

This phase updates the PR quality workflow to look for current changelog evidence and replaces the old Gemini autofix path with a Codex-based root changelog generator. It keeps the existing high-level quality gate active so code PRs still need release-note coverage without relying on retired artifacts.

*Technical detail:* [context.md#phase-11-replace-retired-changelog-gate](./context.md#phase-11-replace-retired-changelog-gate)

**Acceptance criteria**:

- [x] Code PR quality checks no longer require `wf-changelog.md`.
- [x] PR quality workflow output no longer references Gemini autofix, old workflow changelog skills, or `thoughts/` artifacts.
- [x] Code PRs with a root changelog update or Spektacular spec/plan artifacts satisfy the documentation gate.
- [x] Same-repository code PRs labeled `codex-autofix` can generate and commit a root changelog entry with Codex.
- [x] Workflow YAML and local shell checks validate the new gate behavior.

## Open Questions

There are no open questions. Release and nightly workflow cleanup is intentionally separate.

## Out of Scope

- Updating release or nightly release workflows.
- Running Codex autofix on fork PRs.
- Generating Spektacular specs or plans from CI.
- Changing Rust CI, docs CI, coverage, or formatting checks.
- Rebasing open PRs after this workflow fix lands.

## Changelog

### FINAL SUMMARY

The PR quality workflow now checks Rust code changes against the current release-note evidence model: root `CHANGELOG.md` edits or Spektacular spec/plan artifacts. The retired `wf-changelog.md`, Gemini autofix label, Gemini CLI action, and `thoughts/` artifact references are no longer part of the gate output or automation. A maintainer-triggered `codex-autofix` path can generate a root changelog entry for same-repository PRs.

**Total phases**: 1/1 completed

**Notable deviations from the plan**: No committed automated test was added because the repository has no existing convention for testing GitHub Actions workflow shell logic. After initial implementation, the plan was updated to add a same-repository `codex-autofix` path for root changelog generation.

### 2026-05-02 — Phase 1.1: Replace retired changelog gate

**What was done**: The workflow gate was aligned with Spektacular-era release-note evidence by accepting root changelog updates and `.spektacular/specs/` or `.spektacular/plans/` changes. The visible failure path now gives contributors current instructions instead of asking for retired workflow changelog artifacts, and same-repository PRs can use `codex-autofix` to generate a root changelog entry.

**Deviations**: No committed automated test was added because this repository only has Rust test conventions and no existing framework for GitHub Actions workflow shell logic. The final workflow includes a Codex autofix path requested after the original no-generator plan.

**Files changed**:
- `.github/workflows/workflow-quality.yml`
- `.spektacular/plans/20260502165842-workflow-quality-spektacular-alignment/plan.md`

**Discoveries**: Existing tests are Rust integration/CLI tests under `crates/**/tests`; there is no committed workflow-shell test convention to extend.
