# Plan: 20260504125300-runtime-docs-alignment-followup

<!-- Metadata -->
<!-- Created: 2026-05-04T13:06:48Z -->
<!-- Commit: afb7e63 -->
<!-- Branch: main -->
<!-- Repository: https://github.com/weavster-dev/weavster.git -->

## Overview

This plan aligns Weavster's CLI and documentation with the current implemented file-flow runtime. It removes ignored run-mode flags, makes one-shot test execution use the configured project, and updates README/docs/rustdoc language so users can distinguish current behavior from planned trigger-driven work.

## Architecture & Design Decisions

The chosen direction is a behavior-backed current-state cleanup. The CLI should stop exposing `run` options that do not change runtime behavior, and documentation should stop teaching those options as usable workflow controls. This keeps the surface honest without building new trigger, watcher, routing, connector, or lookup features in this pass.

The only behavior correction is that `weavster test` should honor the same global project config selection used by the other project-aware commands. Test discovery and relative fixture paths should resolve from the selected project so a non-default config path actually changes the project under test.

The key trade-off is scope control. We are rejecting a docs-only patch because it would leave ignored CLI options in place, but also rejecting implementation of planned runtime features because the goal is to reset the baseline before building them. See `research.md#alternatives-considered-and-rejected` for the rejected options and evidence.

## Component Breakdown

- **CLI command contract** owns the public command and option surface. It should expose only supported `run` options and route global config consistently into project-aware commands.
- **Test runner project context** owns discovery of YAML tests and fixture paths. It should execute tests against the project selected by the CLI config option rather than the process working directory.
- **Documentation baseline** owns README, docs-site pages, and generated Rust documentation claims. It should describe current file-flow behavior, test execution, and known limits without implying planned runtime features are available.
- **Regression verification** owns proof that removed flags fail, current sample runs still work, config-selected tests work, and docs no longer contain stale run-mode instructions.

## Data Structures & Interfaces

No new serialized data structures are introduced. Existing project, flow, connector, and test YAML shapes remain compatible.

Two internal CLI command interfaces change:

```rust
commands::run::run(config_path, profile)
commands::test::run(config_path, pattern, profile)
```

`TestDefinition` keeps the same YAML contract. The CLI test command normalizes relative `input` and `expected_output` paths against the selected project before handing definitions to the existing test executor.

## Implementation Detail

The implementation follows the existing Clap command structure and avoids new modules or abstractions. Removing the `run` fields from the command enum lets Clap reject `--once` and `--flow` automatically, which is the simplest contract for unsupported flags.

The test command keeps the current compile-and-execute flow but loads configuration from the global CLI config path and discovers tests from the loaded project's base path. This mirrors the rest of the CLI's project-aware behavior and keeps path resolution explicit for test fixtures.

Documentation updates keep the existing status taxonomy and page structure. The pass changes inaccurate claims and examples, adds precise caveats for current runtime limits, and updates docs-site setup commands to match the npm lockfile and CI workflow.

## Dependencies

- **`weavster-cli`** provides the command surface, project config routing, runtime command, and test command; it needs small behavior and integration-test updates.
- **`weavster-core` testing types and executor** provide the existing YAML test model and WASM-backed comparison path; no public test schema changes are needed.
- **`weavster-codegen` and `weavster-runtime`** provide evidence for transform, lookup, conditional output, and file-only runtime limits; no feature implementation changes are needed.
- **Docs-site toolchain** uses npm scripts and an npm lockfile; docs setup instructions should match that existing dependency boundary.
- **Prior current-vs-planned docs plans** provide the status taxonomy and SQLite local-state baseline this follow-up preserves.

## Testing Approach

Testing focuses on CLI contract and project-selection regression. Integration tests should prove the generated file-flow project still runs without removed flags, rejected run flags fail argument parsing, and the test command loads the selected project and relative fixtures.

Documentation verification uses targeted scans plus the normal docs build/typecheck path. Rust formatting, tests, clippy, and rustdoc remain the broader regression guard because this pass touches both CLI behavior and generated API documentation.

## Milestones & Phases

### Milestone 1: CLI matches supported run and test behavior

**What changes**: Users can no longer pass run flags that are ignored, and tests run against the project they selected. The generated file-flow runtime path remains available through `weavster run`, while explicit one-shot validation belongs to `weavster test`.

#### - [x] Phase 1.1: Remove misleading run flags and honor test config

This phase removes unsupported manual run-mode options from the CLI and updates the command wiring around runtime and test execution. It also makes YAML tests discover and read fixtures from the selected project so the global config option behaves consistently. The runtime's current file-flow behavior remains unchanged except that ignored `run` options become invalid.

*Technical detail:* [context.md#phase-11-remove-misleading-run-flags-and-honor-test-config](./context.md#phase-11-remove-misleading-run-flags-and-honor-test-config)

**Acceptance criteria**:

- [x] Runtime execution still works for the generated starter project using the supported `run` command.
- [x] The run command no longer lists or accepts one-shot or per-flow runtime options.
- [x] YAML tests run against the project selected through the global config option.
- [x] Relative test fixture paths resolve from the selected project.
- [x] No new trigger, watcher, routing, connector, or lookup runtime behavior is introduced.

### Milestone 2: Documentation describes the current baseline

**What changes**: The README, docs site, and Rust API docs stop directing users toward unsupported run flags or overstated runtime features. Current behavior is described as file-based runtime execution plus explicit test execution, while partial features remain visible as limits rather than working guarantees.

#### - [x] Phase 2.1: Align docs and status language

This phase removes stale run-mode examples from user-facing docs and replaces them with current commands. It sharpens the documented limits around runtime state config, conditional outputs, lookup transforms, generated transform chaining, and job queue wording. It also updates docs-site contributor commands to match the package manager actually used by the repository.

*Technical detail:* [context.md#phase-21-align-docs-and-status-language](./context.md#phase-21-align-docs-and-status-language)

**Acceptance criteria**:

- [x] README and docs-site examples no longer teach removed run flags.
- [x] One-shot validation is documented as test execution rather than a run mode.
- [x] Current runtime limits are visible for local state path selection, conditional output delivery, lookup artifacts, and generated transform chaining.
- [x] Rust documentation no longer describes unimplemented job queue management as an available runtime feature.
- [x] Docs-site setup instructions use the package manager and scripts already present in the docs project.
- [x] The changelog records the current-state alignment.

## Open Questions

There are no open questions. The scope is current-state alignment only, and the user has already excluded planned trigger-driven runtime behavior from this pass.

## Out of Scope

- Implementing file watchers, event triggers, continuous polling, or trigger-driven runtime startup.
- Implementing per-flow runtime filtering for `weavster run`.
- Implementing conditional routing, output filtering, lookup artifact loading, transform-chaining fixes, or non-file connector runtime execution.
- Changing `runtime.local.data_dir` semantics or moving the runtime SQLite database path.
- Changing package signing, registry push/pull, placeholder management commands, CI coverage thresholds, or docs-site tooling dependencies.

## Changelog

### FINAL SUMMARY

This plan delivered the current-state cleanup requested for Weavster's run/test/docs baseline. The CLI no longer accepts ignored `run` flags, `weavster test` now honors the selected project config, and README/docs/rustdoc wording now reflects the implemented file-flow runtime and known partial features.

**Total phases**: 2/2 completed

**Notable deviations from the plan**: The CLI test positional argument help was corrected from "Test file or pattern" to "Test name filter" because implementation filters by test name, not by file glob.

### 2026-05-04 — Phase 1.1: Remove misleading run flags and honor test config

**What was done**: Removed the unsupported `run --once` and `run --flow` CLI options so they now fail argument parsing instead of being silently ignored. Updated `weavster test` to load the selected project from the global config option, discover that project's tests directory, and resolve relative test fixtures from the project root.

**Deviations**: The test command help text was also corrected from "Test file or pattern" to "Test name filter" after analysis confirmed the implementation filters test names only.

**Files changed**:
- `crates/weavster-cli/src/main.rs`
- `crates/weavster-cli/src/commands/run.rs`
- `crates/weavster-cli/src/commands/test.rs`
- `crates/weavster-cli/tests/cli_test.rs`
- `.spektacular/plans/20260504125300-runtime-docs-alignment-followup/plan.md`

**Discoveries**: `TestDefinition` fixture paths are consumed directly by the core test executor, so CLI-side normalization is the smallest fix that preserves the existing test schema.

### 2026-05-04 — Phase 2.1: Align docs and status language

**What was done**: Updated README and docs-site examples to use `weavster run` without unsupported run-mode flags and documented `weavster test` as the explicit one-shot validation path. Sharpened status wording for local SQLite path selection, conditional output delivery, lookup artifact loading, generated transform chaining, runtime job docs, and docs-site npm commands.

**Deviations**: None.

**Files changed**:
- `README.md`
- `docs/docs/index.md`
- `docs/docs/getting-started/first-flow.md`
- `docs/docs/cli/commands.md`
- `docs/docs/configuration/project.md`
- `docs/docs/configuration/flows.md`
- `docs/docs/concepts/transforms.md`
- `crates/weavster-runtime/src/lib.rs`
- `crates/weavster-runtime/src/jobs.rs`
- `docs/README.md`
- `CHANGELOG.md`
- `.spektacular/plans/20260504125300-runtime-docs-alignment-followup/plan.md`

**Discoveries**: The docs-site build passes but Docusaurus emits a non-fatal update-check warning because the local update config store under the user's home directory is not writable from the process.
