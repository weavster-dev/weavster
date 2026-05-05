# Research: 20260504125300-runtime-docs-alignment-followup

## Alternatives considered and rejected

### Option A: Documentation-only cleanup

This option would remove stale `run --once` and `run --flow` instructions from README/docs while leaving the CLI behavior unchanged.

**Rejected**: The CLI currently defines `run --flow` at `crates/weavster-cli/src/main.rs:78` and `run --once` at `crates/weavster-cli/src/main.rs:82`, but the runtime implementation accepts them as `_flow` and `_once` at `crates/weavster-cli/src/commands/run.rs:14` and `crates/weavster-cli/src/commands/run.rs:15`. Leaving ignored options in the binary would keep the misleading contract alive.

### Option B: Implement event triggers or watch mode now

This option would replace removed run modes with a new event-triggered runtime path or file watcher so `run` starts from events rather than manual flags.

**Rejected**: The spec explicitly limits this pass to current-state alignment and excludes file watching, event triggers, routing, and connector runtime implementation. The current runtime file path is pull-based over available file input records at `crates/weavster-runtime/src/engine.rs:130`, so trigger semantics need their own plan.

### Option C: Implement deeper partial features while documenting them

This option would implement `runtime.local.data_dir`, conditional output routing, lookup artifact loading, or generated transform chaining fixes as part of the cleanup.

**Rejected**: Each item changes runtime behavior beyond the current cleanup scope. The current SQLite path is hardcoded at `crates/weavster-cli/src/commands/run.rs:66`, runtime delivery broadcasts to all outputs at `crates/weavster-runtime/src/engine.rs:151`, lookup transforms are parsed without artifact loading at `crates/weavster-codegen/src/parser.rs:115`, and generated transforms start from a fresh output map at `crates/weavster-codegen/src/generator.rs:168`.

## Chosen approach — evidence

The chosen approach is a narrow CLI contract fix plus documentation alignment. Removing the ignored `run` options makes Clap reject unsupported manual run modes automatically, while keeping `weavster run` available for the current file-flow runtime path.

The test command config fix is included because it is current-state consistency rather than planned runtime behavior. Other project-aware commands pass the global config path into command handlers, but `Commands::Test` currently omits it at `crates/weavster-cli/src/main.rs:229`, and the test command hardcodes `weavster.yaml` at `crates/weavster-cli/src/commands/test.rs:15`.

Docs need updates because the stale CLI surface appears in multiple user-facing entry points: `README.md:43`, `README.md:170`, `docs/docs/index.md:39`, `docs/docs/getting-started/first-flow.md:74`, and `docs/docs/cli/commands.md:55`. Additional status wording needs sharpening because current implementation details are narrower than the docs imply.

## Files examined

- `crates/weavster-cli/src/main.rs:76` — `Run` currently exposes `flow`, `once`, and `profile`.
- `crates/weavster-cli/src/main.rs:197` — run dispatch passes ignored flags into the command handler.
- `crates/weavster-cli/src/main.rs:229` — test dispatch does not pass the global config path.
- `crates/weavster-cli/src/commands/run.rs:12` — runtime command signature accepts unused `_flow` and `_once` parameters.
- `crates/weavster-cli/src/commands/run.rs:66` — local SQLite state path is hardcoded to `.weavster/data/local.db`.
- `crates/weavster-cli/src/commands/test.rs:13` — test command signature lacks a config path.
- `crates/weavster-cli/src/commands/test.rs:15` — test command hardcodes `weavster.yaml`.
- `crates/weavster-cli/src/commands/test.rs:22` — test discovery is relative to the process working directory.
- `crates/weavster-cli/tests/cli_test.rs:4` — current CLI integration test is named around `run --once`.
- `crates/weavster-core/src/config.rs:485` — config loader accepts either a project directory or a `weavster.yaml` path and computes `base_path`.
- `crates/weavster-core/src/testing/models.rs:6` — `TestDefinition` has stable YAML fields for name, flow, input, expected output, and assertions.
- `crates/weavster-core/src/testing/executor.rs:91` — unit test execution reads input fixtures from `TestDefinition.input`.
- `crates/weavster-core/src/testing/executor.rs:104` — expected fixtures are read from `TestDefinition.expected_output`.
- `crates/weavster-runtime/src/engine.rs:90` — conditional outputs are reduced to connector references.
- `crates/weavster-runtime/src/engine.rs:151` — runtime pushes each successful result to every output connector.
- `crates/weavster-runtime/src/engine.rs:214` — input connector runtime support is file-only.
- `crates/weavster-runtime/src/engine.rs:224` — output connector runtime support is file-only.
- `crates/weavster-runtime/src/lib.rs:8` — rustdoc currently claims job queue management as an implemented feature.
- `crates/weavster-runtime/src/jobs.rs:57` — apalis job handler remains a TODO.
- `crates/weavster-codegen/src/generator.rs:126` — static lookup tables are emitted only from existing IR artifacts.
- `crates/weavster-codegen/src/generator.rs:168` — generated transform code starts with a fresh output map.
- `crates/weavster-codegen/src/generator.rs:247` — generated map transforms read from the original source record.
- `crates/weavster-codegen/src/generator.rs:351` — generated lookup transforms reference static tables by lookup table name.
- `crates/weavster-codegen/src/generator.rs:385` — generated filter transform is a pass-through placeholder.
- `crates/weavster-codegen/src/generator.rs:401` — generated coalesce transforms read from the original source record.
- `crates/weavster-codegen/src/parser.rs:115` — lookup transform parsing does not load lookup artifacts.
- `README.md:43` — quick start still instructs `weavster run --once`.
- `README.md:150` — lookup status is too vague about end-to-end limitations.
- `README.md:170` — known limitations still say `run --flow` and `run --once` are accepted.
- `docs/docs/index.md:39` — docs landing quick start still uses `weavster run --once`.
- `docs/docs/getting-started/first-flow.md:74` — first-flow guide still uses `weavster run --once`.
- `docs/docs/getting-started/first-flow.md:90` — first-flow current limits still says `--once` is accepted.
- `docs/docs/cli/commands.md:55` — CLI docs still list `weavster run --once`.
- `docs/docs/cli/commands.md:57` — CLI docs still list `weavster run --flow example_flow`.
- `docs/docs/configuration/project.md:47` — `runtime.local.data_dir` is documented as current runtime behavior.
- `docs/docs/configuration/flows.md:80` — conditional output enforcement is described as partial but not explicit enough.
- `docs/docs/concepts/transforms.md:108` — transform chaining is documented broadly without generated-runtime caveats.
- `docs/README.md:8` — docs-site README uses yarn even though the repo has an npm lockfile.
- `.github/workflows/docs.yml:49` — docs CI installs with npm.
- `docs/package.json:5` — docs project exposes npm scripts.
- `CHANGELOG.md:7` — latest Spektacular entries are timestamped and should receive this cleanup entry.

## External references

None. The current codebase, docs, and prior Spektacular artifacts are sufficient for this cleanup.

## Prior plans / specs consulted

- `.spektacular/specs/20260504125300-runtime-docs-alignment-followup.md` — defines the current-state alignment scope, explicit non-goals, and acceptance criteria.
- `.spektacular/plans/20260428024252-docs-current-vs-planned-alignment/plan.md` — establishes the status taxonomy used across README and docs-site pages.
- `.spektacular/plans/20260428024252-docs-current-vs-planned-alignment/research.md` — provides prior source-backed evidence for file-only runtime and partial transform behavior.
- `.spektacular/plans/20260502160602-sqlite-local-runtime-docs-cleanup/plan.md` — confirms the compatibility-preserving approach for local runtime docs.
- `.spektacular/plans/20260502160602-sqlite-local-runtime-docs-cleanup/research.md` — confirms SQLite local-state wording and preserving config compatibility as prior decisions.

## Open assumptions

No open assumptions. If implementation reveals that a removed run flag is consumed somewhere outside the inspected CLI path, stop and re-evaluate before changing runtime behavior.

## Rehydration cues

Re-read the active spec and plan:

```bash
nl -ba .spektacular/specs/20260504125300-runtime-docs-alignment-followup.md
nl -ba .spektacular/plans/20260504125300-runtime-docs-alignment-followup/plan.md
nl -ba .spektacular/plans/20260504125300-runtime-docs-alignment-followup/context.md
```

Regenerate the main source evidence:

```bash
rg -n "run --once|run --flow|--once|--flow|data_dir|conditional|lookup|job queue|yarn" README.md docs crates CHANGELOG.md
nl -ba crates/weavster-cli/src/main.rs
nl -ba crates/weavster-cli/src/commands/run.rs
nl -ba crates/weavster-cli/src/commands/test.rs
nl -ba crates/weavster-cli/tests/cli_test.rs
```
