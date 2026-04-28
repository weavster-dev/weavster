# Plan: 20260428024252-docs-current-vs-planned-alignment

<!-- Metadata -->
<!-- Created: 2026-04-28T02:51:06Z -->
<!-- Commit: aef8570 -->
<!-- Branch: convert-to-specktacular -->
<!-- Repository: https://github.com/weavster-dev/weavster.git -->

## Overview

This plan realigns Weavster's project documentation so readers can quickly tell what works today, what is partial, and what remains planned. It prevents users from following stale setup instructions or treating roadmap items as production-ready behavior, while giving contributors a clear baseline for documentation and test health.

## Architecture & Design Decisions

The chosen direction is a full documentation rewrite split into two phases: first establish the top-level project documentation as the canonical current-vs-planned status baseline, then bring the docs site into alignment with that baseline. This intentionally does more than a README-only cleanup because the docs site currently contains older syntax and unsupported command claims that would continue to mislead users if left untouched.

The key trade-off is scope control. The plan allows broad documentation correction, but keeps it documentation-only: no runtime behavior, CLI behavior, CI thresholds, or release mechanics change. Status labels become the contract between pages so future readers can scan whether a capability is current, partial, config-only, placeholder, or planned.

This direction beats a README-only pass because it fixes both the high-traffic entry point and the detailed pages users are likely to follow next. It also avoids a design or branding rewrite by limiting changes to factual alignment and reader navigation. See `research.md#alternatives-considered-and-rejected` for the options that were rejected.

## Component Breakdown

- **Top-level project documentation** owns the canonical status model, quick-start path, current limitations, planned roadmap, and test-health summary. The docs site should defer to this baseline rather than contradict it.
- **Getting-started documentation** owns the first successful user path after installation. It must describe the generated sample project shape and syntax that the current CLI actually creates.
- **Configuration documentation** owns the current project, flow, profile, connector-reference, and macro concepts. It must separate supported config parsing from runtime support.
- **Concept documentation** owns explanatory material for connectors and transforms. It must state the difference between parsed configuration, generated WASM support, interpreter support, and end-to-end runtime support.
- **CLI reference documentation** owns command availability and behavior. It must include implemented commands, placeholders, and unsupported commands without presenting missing behavior as available.
- **Verification notes** own the audit trail for test and docs health. They connect the status model back to source evidence and local verification results.

## Data Structures & Interfaces

No code data structures, public APIs, command signatures, or serialization formats change in this plan.

The only documentation contract introduced is a status taxonomy used consistently across the README and docs site:

```text
current     - usable today in the documented workflow
partial     - implemented in some layers but limited or incomplete
config-only - parsed or modeled, but not executed end-to-end
placeholder - command or surface exists but intentionally does no useful work yet
planned     - roadmap item with no current usable behavior
```

## Implementation Detail

Phase 1 rewrites the top-level project documentation to set the taxonomy and user-facing baseline. It should be concise enough for a new reader to understand the product state, but explicit enough that contributors can see where code, docs, tests, and roadmap diverge.

Phase 2 rewrites the docs site pages that currently drift from the code. The docs should reuse the same status taxonomy, update examples to current syntax, and mark limited areas instead of removing all mention of planned features. This keeps the product direction visible while preventing unsupported instructions from appearing operational.

No new documentation framework or site structure is introduced. The work follows the existing Markdown and Docusaurus page structure, with content changes scoped to factual status, examples, command references, and verification notes.

## Dependencies

- **Completed specification**: Defines the documentation-only scope, acceptance criteria, and non-goals. No changes needed.
- **Existing source code and CLI behavior**: Provides the evidence for current, partial, config-only, placeholder, and planned labels. No changes needed.
- **Existing docs site toolchain**: Provides the current documentation build system. No package changes should be needed.
- **Existing CI and coverage configuration**: Provides the test-health claims that documentation will summarize. No workflow or threshold changes are part of this plan.

## Testing Approach

This is documentation-only work, so the primary verification is documentation review against source-backed status claims. No new Rust unit or integration tests are planned because no behavior changes.

Validation should confirm that the documented quick-start path matches the generated sample project, unsupported commands and connectors are not presented as ready, and old claims about embedded PostgreSQL, push/pull commands, inline connector syntax, and full non-file runtime support are removed or clearly marked.

The existing code checks still matter as regression guards. They should pass before completion to prove the documentation update did not accidentally alter source behavior or formatting.

## Milestones & Phases

### Milestone 1: README becomes the product status baseline

**What changes**: The top-level project documentation tells users what Weavster can do today, where it is partial, and what is planned. A new reader can follow the current source-based quick start and understand the core limitations before using the CLI.

#### - [x] Phase 1.1: Reorganize the README around current and planned capabilities

This phase rewrites the top-level project documentation into a current-state entry point. It replaces aspirational quick-start and feature claims with a working source-based path, feature status table, known limitations, roadmap, and verification summary. It keeps future direction visible but separates it from what users can rely on today.

*Technical detail:* [context.md#phase-11-reorganize-the-readme-around-current-and-planned-capabilities](./context.md#phase-11-reorganize-the-readme-around-current-and-planned-capabilities)

**Acceptance criteria**:

- [x] The top-level documentation clearly labels current, partial, config-only, placeholder, and planned capabilities.
- [x] The quick-start path describes the current source-based install and generated example project shape.
- [x] Runtime connector, transform, packaging, and state-storage limitations are visible before the roadmap section.
- [x] Test and coverage-policy status are summarized without claiming unmeasured local coverage.

### Milestone 2: Docs site matches the README baseline

**What changes**: The docs site no longer contradicts the top-level project documentation. Detailed pages use current syntax, mark limited behavior, and avoid documenting missing commands or unsupported runtime integrations as available.

#### - [x] Phase 2.1: Align the docs site with the status baseline

This phase updates the getting-started, configuration, connector, transform, and CLI reference pages to match the README's current-vs-planned model. It replaces stale examples with current connector-reference syntax and marks config-only or placeholder surfaces clearly. It also removes or relabels unsupported command references so users do not try commands that do not exist.

*Technical detail:* [context.md#phase-21-align-the-docs-site-with-the-status-baseline](./context.md#phase-21-align-the-docs-site-with-the-status-baseline)

**Acceptance criteria**:

- [x] The docs site quick start matches the generated sample project and current local runtime behavior.
- [x] Configuration examples use connector references and transform syntax accepted by the current code.
- [x] Connector and transform pages distinguish parsing/codegen support from end-to-end runtime support.
- [x] CLI docs list implemented commands and placeholders accurately, with no unsupported push, pull, or quiet option claims.
- [x] The documentation set can be reviewed without finding unmarked claims that non-file runtime connectors, embedded PostgreSQL local runtime, or package signing are complete today.

## Open Questions

There are no open questions. The implementation scope, phase split, status taxonomy, and documentation-only boundary have been decided.

## Out of Scope

- Implementing Kafka, PostgreSQL, HTTP, Bridge, filter, conditional routing, push/pull, status, flow management, connector management, or package signing behavior.
- Changing the CLI interface, runtime architecture, test suite, CI workflows, or coverage thresholds.
- Rebranding the project or redesigning the documentation site.
- Publishing hosted install scripts, release binaries, or external website changes.

## Changelog

### 2026-04-28 — Phase 1.1: Reorganize the README around current and planned capabilities

**What was done**: Rewrote the top-level README as the canonical current-vs-planned status baseline. The README now includes source-based quick start instructions, current example flow syntax, feature status tables, known limitations, roadmap items, project structure, and verification/coverage-policy notes.

**Deviations**: None.

**Files changed**:
- `README.md`
- `.spektacular/plans/20260428024252-docs-current-vs-planned-alignment/plan.md`

**Discoveries**: No new source behavior was discovered beyond the plan research. The README now explicitly calls out that generated `profiles.yaml` is not aligned with current config loading.

### 2026-04-28 — Phase 2.1: Align the docs site with the status baseline

**What was done**: Rewrote the docs-site introduction, installation, first-flow, project configuration, flow configuration, connector, transform, and CLI command pages to match the README status baseline. The docs now use current connector-reference syntax, document the generated sample project shape, and label partial, config-only, placeholder, and planned surfaces instead of presenting them as complete behavior.

**Deviations**: None.

**Files changed**:
- `docs/docs/index.md`
- `docs/docs/getting-started/installation.md`
- `docs/docs/getting-started/first-flow.md`
- `docs/docs/configuration/project.md`
- `docs/docs/configuration/flows.md`
- `docs/docs/concepts/connectors.md`
- `docs/docs/concepts/transforms.md`
- `docs/docs/cli/commands.md`
- `.spektacular/plans/20260428024252-docs-current-vs-planned-alignment/plan.md`

**Discoveries**: Docusaurus build and typecheck are available locally through `docs/node_modules` and both pass. A targeted scan still finds expected planned or negated mentions of `weavster push`, `weavster pull`, and `--quiet`; those are intentionally labeled rather than removed.
