# Research: 20260428024252-docs-current-vs-planned-alignment

## Alternatives considered and rejected

### Option A: README only

Rewrite only the top-level README and leave the docs site untouched.

**Rejected**: This would fix the main entry point but leave stale detailed instructions in place. The docs site currently claims embedded PostgreSQL local runtime at `docs/docs/getting-started/first-flow.md:57`, uses inline connector syntax at `docs/docs/getting-started/first-flow.md:35`, and documents missing push/pull commands at `docs/docs/cli/commands.md:88`.

### Option B: Narrow correction of only incorrect lines

Patch only the known inaccurate claims without reorganizing the documentation around a status taxonomy.

**Rejected**: This would reduce some obvious errors but would not give users a durable way to understand current, partial, config-only, placeholder, and planned features. The source has multiple implementation layers, such as connector parsing in `crates/weavster-core/src/connectors.rs:63` versus runtime file-only support in `crates/weavster-runtime/src/engine.rs:214`, so status labels are necessary.

### Option C: Full documentation rewrite with site redesign

Use this effort to redesign the entire documentation site, navigation, and product positioning.

**Rejected**: The spec explicitly keeps the work documentation-only and factual. A broader redesign would create avoidable churn and is not required to fix stale current-vs-planned feature claims.

## Chosen approach - evidence

The chosen approach is a full factual documentation rewrite in two phases: README first, docs site second.

Evidence for README-first: `README.md:13` claims embedded PostgreSQL local dev, `README.md:20` uses a hosted install script, `README.md:45` shows Kafka/Postgres and filter/template behavior in the main example, and `README.md:105` says local runtime uses embedded PostgreSQL. These claims define the initial user expectation and must be corrected first.

Evidence for docs-site alignment: `docs/docs/getting-started/first-flow.md:22` names the wrong generated flow file, `docs/docs/configuration/project.md:16` documents stale runtime/database fields, `docs/docs/configuration/flows.md:15` uses inline connector config, `docs/docs/concepts/connectors.md:11` presents non-file connectors as available, `docs/docs/concepts/transforms.md:23` uses old transform syntax, and `docs/docs/cli/commands.md:88` lists unsupported commands.

Evidence for status taxonomy: connector configs parse file, HTTP, Kafka, Postgres, and Bridge at `crates/weavster-core/src/connectors.rs:63`, but runtime execution rejects all non-file connectors at `crates/weavster-runtime/src/engine.rs:214` and `crates/weavster-runtime/src/engine.rs:224`. Transform config parsing accepts broad transform types at `crates/weavster-core/src/transforms.rs:74`, while the direct interpreter rejects regex, template, lookup, and filter at `crates/weavster-core/src/interpreter.rs:97`, and generated filter behavior is placeholder at `crates/weavster-codegen/src/generator.rs:372`.

Evidence for test-health summary: `crates/weavster-cli/tests/cli_test.rs:3` verifies init plus run-once JSONL output, `.github/workflows/ci.yml:47` runs all-features tests, `.github/workflows/coverage.yml:30` generates coverage, and `.octocov.yml:7` sets acceptable coverage at 60%.

## Files examined

- `README.md:13` - Current README overstates embedded PostgreSQL local dev.
- `README.md:20` - Quick start uses a hosted install script not verified by the repo.
- `README.md:45` - Example flow uses non-file connectors and incomplete behaviors.
- `README.md:105` - Runtime mode table says local backend is embedded PostgreSQL.
- `Cargo.toml:12` - Workspace declares Rust `1.92.0`.
- `crates/weavster-cli/src/main.rs:30` - Actual CLI command list includes init, compile, package, run, validate, status, flow, connector, test.
- `crates/weavster-cli/src/commands/init.rs:40` - Init creates flows, connectors, and tests directories.
- `crates/weavster-cli/src/commands/init.rs:64` - Init writes `example_flow.yaml`.
- `crates/weavster-cli/src/commands/init.rs:68` - Generated flow uses connector reference syntax.
- `crates/weavster-cli/src/commands/init.rs:87` - Generated connectors live in `connectors/file.yaml`.
- `crates/weavster-cli/src/commands/init.rs:125` - Init writes `profiles.yaml`, but current config loader expects profiles in `weavster.yaml`.
- `crates/weavster-cli/src/commands/run.rs:53` - Runtime uses Postgres state only when `WEAVSTER_PG_URL` exists.
- `crates/weavster-cli/src/commands/run.rs:65` - Local state store is SQLite.
- `crates/weavster-cli/src/commands/validate.rs:17` - Validate has TODOs for flow, connector, and expression validation.
- `crates/weavster-cli/src/commands/status.rs:16` - Status command prints "Not yet implemented".
- `crates/weavster-cli/src/commands/flow.rs:9` - Flow list command is not implemented.
- `crates/weavster-cli/src/commands/connector.rs:9` - Connector list command is not implemented.
- `crates/weavster-cli/src/commands/package.rs:107` - Package manifest digest is a placeholder.
- `crates/weavster-cli/src/commands/package.rs:126` - Package signing is not implemented.
- `crates/weavster-runtime/src/engine.rs:214` - Runtime input connectors are file-only.
- `crates/weavster-runtime/src/engine.rs:224` - Runtime output connectors are file-only.
- `crates/weavster-core/src/connectors.rs:63` - Connector config enum parses file, HTTP, Kafka, Postgres, and Bridge.
- `crates/weavster-core/src/transforms.rs:74` - Transform config enum parses the full current transform vocabulary.
- `crates/weavster-core/src/interpreter.rs:91` - Direct interpreter supports map, drop, add_fields, and coalesce.
- `crates/weavster-core/src/interpreter.rs:97` - Direct interpreter rejects regex, template, lookup, and filter.
- `crates/weavster-codegen/src/generator.rs:174` - Codegen dispatches generated code for all transform IR variants.
- `crates/weavster-codegen/src/generator.rs:372` - Filter transform generation is a pass-through placeholder.
- `crates/weavster-codegen/src/generator.rs:417` - Conditional output filter functions are generated separately and expression handling returns true.
- `crates/weavster-cli/tests/cli_test.rs:3` - End-to-end CLI test covers generated sample project.
- `.github/workflows/ci.yml:22` - CI checks formatting.
- `.github/workflows/ci.yml:33` - CI runs clippy with warnings denied.
- `.github/workflows/ci.yml:47` - CI runs all-features tests.
- `.github/workflows/ci.yml:58` - CI builds Rust docs with warnings denied.
- `.github/workflows/coverage.yml:30` - CI generates coverage using cargo-llvm-cov.
- `.octocov.yml:7` - Coverage acceptable threshold is 60%.
- `docs/docs/index.md:15` - Docs intro claims embedded PostgreSQL local dev.
- `docs/docs/getting-started/installation.md:10` - Installation page already frames pre-built binaries as coming soon.
- `docs/docs/getting-started/first-flow.md:22` - First-flow page lists stale generated file shape.
- `docs/docs/getting-started/first-flow.md:35` - First-flow page uses inline connector syntax.
- `docs/docs/getting-started/first-flow.md:57` - First-flow page says embedded PostgreSQL starts.
- `docs/docs/configuration/project.md:16` - Project config page documents stale runtime and database fields.
- `docs/docs/configuration/flows.md:15` - Flow config page uses stale inline connector examples.
- `docs/docs/concepts/connectors.md:11` - Connector page presents Kafka and other integrations without current runtime limits.
- `docs/docs/concepts/transforms.md:23` - Transform page uses old `type`/`fields` syntax.
- `docs/docs/cli/commands.md:88` - CLI docs include missing push command.
- `docs/docs/cli/commands.md:96` - CLI docs include missing pull command.
- `docs/docs/cli/commands.md:111` - CLI docs include missing quiet option.

## External references

None. This plan is based on repository source, tests, and documentation.

## Prior plans / specs consulted

- `.spektacular/specs/20260428024252-docs-current-vs-planned-alignment.md` - Defines documentation-only scope, acceptance criteria, and non-goals.
- `.spektacular/knowledge/conventions.md` - Notes documentation updates for user-facing changes, but contains generic/non-Rust guidance and does not override the repo instructions.
- `.spektacular/knowledge/architecture/README.md` - Placeholder only; no usable architecture guidance.
- `.spektacular/knowledge/gotchas/README.md` - Placeholder only; no relevant gotchas.
- `.spektacular/knowledge/learnings/README.md` - Placeholder only; no relevant learnings.

## Open assumptions

No open assumptions. The implementation should stop and ask only if source code changes underneath the plan make the documented status labels inaccurate.

## Rehydration cues

- Re-read `.spektacular/specs/20260428024252-docs-current-vs-planned-alignment.md`.
- Re-read `README.md` and docs pages under `docs/docs/`.
- Re-read CLI/runtime evidence in `crates/weavster-cli/src/commands/`, `crates/weavster-runtime/src/engine.rs`, `crates/weavster-core/src/connectors.rs`, `crates/weavster-core/src/transforms.rs`, and `crates/weavster-codegen/src/generator.rs`.
- Re-run local verification after implementation: formatting, all-features tests, clippy, Rust docs, and docs-site build if dependencies are available.
