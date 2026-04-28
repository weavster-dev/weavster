# Context: 20260428024252-docs-current-vs-planned-alignment

## Current State Analysis

The current README is partly aspirational. It claims zero-config local development with embedded PostgreSQL at `README.md:13`, a hosted install script at `README.md:20`, a flow example that uses non-file connectors and filter/template behavior at `README.md:45`, and local runtime mode as embedded PostgreSQL at `README.md:105`. The current CLI/runtime path instead compiles flows to WASM and uses SQLite locally unless `WEAVSTER_PG_URL` is set.

The generated sample project is the strongest source of truth for the first-user path. `crates/weavster-cli/src/commands/init.rs:40` creates `flows`, `connectors`, and `tests`; `crates/weavster-cli/src/commands/init.rs:64` writes `flows/example_flow.yaml`; `crates/weavster-cli/src/commands/init.rs:68` uses `input: file.input`; `crates/weavster-cli/src/commands/init.rs:87` writes `connectors/file.yaml`; and `crates/weavster-cli/src/commands/init.rs:117` writes JSONL sample input.

The runtime only instantiates file connectors for end-to-end execution. `crates/weavster-runtime/src/engine.rs:214` creates file input connectors and rejects other connector types; `crates/weavster-runtime/src/engine.rs:224` does the same for output connectors. `crates/weavster-cli/src/commands/run.rs:53` chooses Postgres only when `WEAVSTER_PG_URL` exists, and `crates/weavster-cli/src/commands/run.rs:65` otherwise uses SQLite local state.

The connector enum parses more than the runtime can execute. `crates/weavster-core/src/connectors.rs:63` defines file, HTTP, Kafka, Postgres, and Bridge connector configs, but only file connectors have implemented input/output connector structs in the same module and only file connectors are accepted by the runtime.

Transform support has multiple layers. `crates/weavster-core/src/transforms.rs:74` parses map, regex, template, lookup, filter, drop, coalesce, and add_fields configs. `crates/weavster-core/src/interpreter.rs:91` directly interprets map, drop, add_fields, and coalesce, while `crates/weavster-core/src/interpreter.rs:97` through `crates/weavster-core/src/interpreter.rs:111` rejects regex, template, lookup, and filter in the interpreter. `crates/weavster-codegen/src/generator.rs:174` dispatches generated WASM code for all transform variants, but `crates/weavster-codegen/src/generator.rs:372` makes filter a pass-through placeholder and `crates/weavster-codegen/src/generator.rs:417` generates conditional output filter functions that always return true for expression conditions.

Several CLI surfaces are placeholders. `crates/weavster-cli/src/commands/validate.rs:17` has TODOs for flow, connector, and expression validation. `crates/weavster-cli/src/commands/status.rs:16`, `crates/weavster-cli/src/commands/flow.rs:9`, and `crates/weavster-cli/src/commands/connector.rs:9` print "Not yet implemented" messages. `crates/weavster-cli/src/commands/package.rs:107` writes a placeholder digest and `crates/weavster-cli/src/commands/package.rs:126` notes signing is not implemented.

The docs site is stale relative to the current code. `docs/docs/getting-started/first-flow.md:22` says init creates `example.yaml`, but current init creates `example_flow.yaml`; `docs/docs/getting-started/first-flow.md:35` uses inline connector syntax; and `docs/docs/getting-started/first-flow.md:57` says an embedded PostgreSQL instance starts. `docs/docs/configuration/project.md:16` documents `runtime.workers`, `runtime.log_level`, and a `database` object that are not the current config shape. `docs/docs/configuration/flows.md:15` uses inline connector config rather than connector references. `docs/docs/cli/commands.md:88` documents missing `push` and `pull` commands, and `docs/docs/cli/commands.md:111` documents a missing `--quiet` global option.

Tests currently pass when generated WASM crates can resolve dependencies. `crates/weavster-cli/tests/cli_test.rs:3` covers init plus run-once and verifies JSONL output. CI runs formatting, clippy, all-features tests, and docs with warnings denied at `.github/workflows/ci.yml:22`, `.github/workflows/ci.yml:33`, `.github/workflows/ci.yml:47`, and `.github/workflows/ci.yml:58`. Coverage is generated in CI with cargo-llvm-cov at `.github/workflows/coverage.yml:30`, but `.octocov.yml:7` sets acceptable coverage at 60%, which conflicts with the repository instruction requiring 90% unit test coverage.

## Per-Phase Technical Notes

### Phase 1.1: Reorganize the README around current and planned capabilities

- `README.md:7` - Reframe the introduction to avoid implying production-ready ESB breadth where runtime support is currently file-based.
- `README.md:13` - Replace the embedded PostgreSQL local-dev claim with SQLite local state plus optional Postgres state via environment.
- `README.md:16` - Replace hosted install-script quick start with source-based installation or build instructions.
- `README.md:40` - Replace the example flow with current generated syntax: connector references, file input/output, and transforms that work in the generated happy path.
- `README.md:79` - Replace the flat feature list with a status matrix covering CLI, runtime connectors, transforms, configuration, packaging, state stores, testing, and docs.
- `README.md:101` - Replace runtime mode table so local state and remote/Redis plans are accurately separated.
- `README.md:108` - Update prerequisites to match workspace metadata, which currently declares Rust `1.92.0` in `Cargo.toml:12`; avoid older Rust claims unless intentionally supported.
- `README.md:127` - Update project structure to include `weavster-runtime` and avoid presenting `weavster-python` as an existing workspace member.

**Complexity**: Medium
**Token estimate**: ~12k
**Agent strategy**: Single agent, sequential execution. This phase is editorial and benefits from one consistent voice.

### Phase 2.1: Align the docs site with the status baseline

- `docs/docs/index.md:13` - Keep the core concept framing, but replace embedded PostgreSQL and broad connector implications with current local runtime and status language.
- `docs/docs/getting-started/installation.md:10` - Keep source installation as the current path; avoid pre-built binary promises except as planned.
- `docs/docs/getting-started/first-flow.md:16` - Correct generated project structure to `flows/example_flow.yaml`, `connectors/file.yaml`, `data/input.jsonl`, and the tests directory.
- `docs/docs/getting-started/first-flow.md:35` - Replace inline connector flow example with connector-reference syntax and generated transform shape.
- `docs/docs/getting-started/first-flow.md:57` - Replace embedded PostgreSQL runtime description with compile-to-WASM, file processing, and SQLite local state.
- `docs/docs/configuration/project.md:16` - Replace stale `runtime.workers`, `runtime.log_level`, and `database` schema with current `runtime.mode`, `runtime.local`, `runtime.remote`, `vars`, inline `profiles`, `error_handling`, and `macros_dir`.
- `docs/docs/configuration/flows.md:15` - Replace inline connector examples with `input: file.input` and `outputs: - file.output`; mark conditional outputs and non-file connectors by current status.
- `docs/docs/concepts/connectors.md:11` - Rework connector sections to show current runtime support for file, config-only parse support for Kafka/Postgres/HTTP/Bridge, and current connector YAML file shape.
- `docs/docs/concepts/transforms.md:17` - Rework transform sections around accepted YAML shape and status by layer: runtime generated path, direct interpreter, incomplete filter behavior.
- `docs/docs/cli/commands.md:7` - Update command list to match actual `weavster --help`: init, compile, package, run, validate, status, flow, connector, test.
- `docs/docs/cli/commands.md:88` - Remove or relabel missing push and pull commands.
- `docs/docs/cli/commands.md:111` - Remove the missing `--quiet` option.

**Complexity**: Medium
**Token estimate**: ~20k
**Agent strategy**: Single agent preferred for consistency; parallel agents are possible only if explicitly requested and write scopes are split by docs section.

## Testing Strategy

Because this plan changes documentation only, verification should combine docs review with existing build checks. The implementer should run the existing Rust checks to ensure no accidental code changes break behavior, then build the docs site if dependencies are available.

At minimum, validate that no stale claims remain for embedded PostgreSQL local runtime, missing push/pull commands, inline flow connector syntax as the primary current form, missing `--quiet`, or complete non-file runtime connectors. If docs-site dependencies are unavailable, record that explicitly and still run the markdown/source checks that do not require network access.

## Project References

- Spec: `.spektacular/specs/20260428024252-docs-current-vs-planned-alignment.md`
- README: `README.md`
- Docs site pages: `docs/docs/index.md`, `docs/docs/getting-started/installation.md`, `docs/docs/getting-started/first-flow.md`, `docs/docs/configuration/project.md`, `docs/docs/configuration/flows.md`, `docs/docs/concepts/connectors.md`, `docs/docs/concepts/transforms.md`, `docs/docs/cli/commands.md`
- Runtime evidence: `crates/weavster-cli/src/commands/run.rs`, `crates/weavster-runtime/src/engine.rs`
- CLI evidence: `crates/weavster-cli/src/main.rs`, `crates/weavster-cli/src/commands/*`
- Test evidence: `crates/weavster-cli/tests/cli_test.rs`, `.github/workflows/ci.yml`, `.github/workflows/coverage.yml`, `.octocov.yml`

## Token Management Strategy

| Tier | Token Budget | Agent Strategy |
|------|-------------|----------------|
| Low | ~10k | Single agent, sequential |
| Medium | ~25k | Single agent preferred for voice consistency; split only if requested |
| High | ~50k+ | Parallel analysis, sequential integration |

## Migration Notes

No user migration is introduced. The docs should explain the current generated project shape and current limitations; they should not require any code or config migration.

## Performance Considerations

No runtime performance impact is expected because this plan changes documentation only. Docs build time may change trivially due to content size, but no new tooling or heavy assets are planned.
