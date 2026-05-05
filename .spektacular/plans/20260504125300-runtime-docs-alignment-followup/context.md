# Context: 20260504125300-runtime-docs-alignment-followup

## Current State Analysis

The CLI currently exposes `run --flow` and `run --once` even though the runtime command ignores both values. `crates/weavster-cli/src/main.rs:76` defines the `Run` subcommand, `crates/weavster-cli/src/main.rs:78` defines `flow`, `crates/weavster-cli/src/main.rs:82` defines `once`, and `crates/weavster-cli/src/commands/run.rs:12` accepts `_flow` and `_once` parameters that are intentionally unused.

The test command is project-aware in the docs but not in the implementation. `crates/weavster-cli/src/main.rs:229` dispatches `Commands::Test` without passing `cli.config`, while `crates/weavster-cli/src/commands/test.rs:13` hardcodes `weavster.yaml` and `crates/weavster-cli/src/commands/test.rs:22` discovers `tests/` relative to the current working directory.

The README and docs site still teach removed or misleading run modes. `README.md:43`, `docs/docs/index.md:39`, and `docs/docs/getting-started/first-flow.md:74` show `weavster run --once`; `docs/docs/cli/commands.md:55` and `docs/docs/cli/commands.md:57` show `run --once` and `run --flow`; `README.md:170` says those flags are accepted.

Current runtime limits are more specific than the docs state. `docs/docs/configuration/project.md:47` marks `runtime.local.data_dir` current, but `crates/weavster-cli/src/commands/run.rs:66` hardcodes `.weavster/data/local.db`. `crates/weavster-runtime/src/engine.rs:90` reduces conditional outputs to connector references and `crates/weavster-runtime/src/engine.rs:151` broadcasts each result to every output. `crates/weavster-codegen/src/generator.rs:126` only emits lookup tables already present in IR artifacts, while `crates/weavster-codegen/src/parser.rs:115` parses lookup transforms without loading artifacts. `crates/weavster-codegen/src/generator.rs:168` builds a fresh output map, and transform generators such as map and coalesce read from the original source record.

Internal Rust documentation overstates unfinished runtime capabilities. `crates/weavster-runtime/src/lib.rs:8` lists job queue management as a runtime feature, while `crates/weavster-runtime/src/jobs.rs:57` still has the apalis job handler as a TODO.

The docs site uses npm in CI and has an npm lockfile, but its local README still shows yarn commands. `docs/package.json:5` defines npm scripts, `docs/package-lock.json` is present, `.github/workflows/docs.yml:49` installs with `npm ci`, and `docs/README.md:8` starts with `yarn`.

## Per-Phase Technical Notes

### Phase 1.1: Remove misleading run flags and honor test config

- `crates/weavster-cli/src/main.rs:76` — remove the `flow` and `once` fields from `Commands::Run`, leaving only `profile`.
- `crates/weavster-cli/src/main.rs:197` — update run dispatch to call `commands::run::run(&cli.config, profile.as_deref())`.
- `crates/weavster-cli/src/main.rs:229` — update test dispatch to call `commands::test::run(&cli.config, pattern.as_deref(), profile.as_deref())`.
- `crates/weavster-cli/src/commands/run.rs:12` — simplify the runtime command signature to remove the unused `_flow` and `_once` parameters.
- `crates/weavster-cli/src/commands/test.rs:13` — change the command signature to accept `config_path`.
- `crates/weavster-cli/src/commands/test.rs:16` — load config from `config_path` instead of a hardcoded `weavster.yaml`.
- `crates/weavster-cli/src/commands/test.rs:22` — discover tests from `config.base_path.join("tests")`.
- `crates/weavster-cli/src/commands/test.rs:39` — after parsing a `TestDefinition`, normalize relative `input` and `expected_output` paths against `config.base_path` before pushing the test.
- `crates/weavster-cli/tests/cli_test.rs:4` — rename the starter-project test away from `run_once` and run the generated project without `--once`.
- `crates/weavster-cli/tests/cli_test.rs` — add regression assertions that `weavster run --once` and `weavster run --flow example_flow` fail argument parsing.
- `crates/weavster-cli/tests/cli_test.rs` — add or extend an integration test that creates a project, writes a YAML test and expected JSONL fixture with project-relative paths, and verifies `weavster --config <project> test` succeeds from outside that project.

**Complexity**: Medium
**Token estimate**: ~18k
**Agent strategy**: Single agent, sequential execution. The write set is small and coupled enough that parallel edits would add merge overhead without saving much time.

### Phase 2.1: Align docs and status language

- `README.md:43` — replace `weavster run --once` with `weavster run`.
- `README.md:150` — clarify that lookup transforms are not end-to-end usable through normal CLI compilation until lookup data is loaded into generated artifacts.
- `README.md:152` — clarify that conditional outputs are parsed but runtime delivery currently sends each successful record to all configured outputs.
- `README.md:170` — remove the claim that `run --flow` and `run --once` are accepted; replace it with a limit that `weavster run` has no one-shot or per-flow mode flags.
- `docs/docs/index.md:39` — replace `weavster run --once` with `weavster run`.
- `docs/docs/getting-started/first-flow.md:74` — replace `weavster run --once` with `weavster run`.
- `docs/docs/getting-started/first-flow.md:90` — remove the statement that `--once` is accepted and describe the current run command as processing available input records and exiting.
- `docs/docs/cli/commands.md:14` — remove "limited option semantics" from the run command status.
- `docs/docs/cli/commands.md:55` — remove `run --once` and `run --flow` examples; keep `run` and profile usage.
- `docs/docs/cli/commands.md:62` — remove `--once` and `--flow` from the run options table.
- `docs/docs/cli/commands.md:110` — document `weavster test` as the one-shot validation command and note that the global config option selects the project.
- `docs/docs/configuration/project.md:47` — change `runtime.local.data_dir` from current runtime behavior to parsed/config-only status.
- `docs/docs/configuration/project.md:58` — state that the current CLI runtime uses `.weavster/data/local.db` and does not use `runtime.local.data_dir` to choose the SQLite path.
- `docs/docs/configuration/flows.md:47` — keep the starter transform ordering language but point detailed chaining caveats to the transforms page.
- `docs/docs/configuration/flows.md:80` — state that conditional output `when` expressions are parsed but not enforced by current runtime delivery.
- `docs/docs/concepts/transforms.md:19` — sharpen lookup status to say artifact loading is not wired into normal CLI codegen.
- `docs/docs/concepts/transforms.md:104` — keep filter and conditional routing caveats explicit.
- `docs/docs/concepts/transforms.md:108` — clarify direct interpreter versus generated runtime chaining limits so users know current starter transforms are the reliable path.
- `crates/weavster-runtime/src/lib.rs:8` — replace the job queue management feature claim with implemented runtime capabilities.
- `crates/weavster-runtime/src/jobs.rs:1` — describe the module as job data models or future queue integration rather than implemented apalis jobs.
- `docs/README.md:8` — replace yarn instructions with npm commands matching the lockfile and docs CI.
- `CHANGELOG.md:7` — add a Spektacular entry for this current-state alignment.

**Complexity**: Medium
**Token estimate**: ~20k
**Agent strategy**: Single agent, sequential execution. Documentation edits should be reviewed together to keep status wording consistent across README and docs pages.

## Testing Strategy

Phase 1.1 needs CLI integration regression coverage. The essential checks are that the generated starter project still produces the expected JSONL output with `weavster run`, removed flags fail parsing, and the test command works when the project is selected through the global config option.

Phase 2.1 needs documentation and rustdoc verification. Targeted scans should confirm removed run flags are gone from README/docs instructions, while docs build/typecheck and Rust docs catch Markdown, TypeScript, and generated documentation regressions.

## Project References

- `.spektacular/specs/20260504125300-runtime-docs-alignment-followup.md` — approved scope, requirements, non-goals, and acceptance criteria for this follow-up.
- `.spektacular/plans/20260428024252-docs-current-vs-planned-alignment/plan.md` — established the current/partial/config-only/placeholder/planned status taxonomy.
- `.spektacular/plans/20260502160602-sqlite-local-runtime-docs-cleanup/plan.md` — established the local SQLite state documentation baseline and compatibility-preserving cleanup approach.
- `AGENTS.md` — requires Spektacular planning before implementation and docs updates for behavior/interface changes.

## Token Management Strategy

| Tier | Token Budget | Agent Strategy |
|------|-------------|----------------|
| Low | ~10k | Single agent, sequential |
| Medium | ~25k | 2-3 parallel agents |
| High | ~50k+ | Parallel analysis, sequential integration |

This work is medium by breadth but not by algorithmic risk. Keep it single-agent unless implementation discovers unexpectedly broad test fixture changes.

## Migration Notes

Existing scripts or users that pass `weavster run --once` or `weavster run --flow` must stop passing those flags. The supported replacement for the generated local file-flow path is `weavster run`; explicit one-shot validation should use `weavster test`.

## Performance Considerations

No runtime performance changes are planned. Test command path normalization is filesystem path handling only and should not affect flow execution performance.
