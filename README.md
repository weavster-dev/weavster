# Weavster

Config-driven integration tool: define transformation pipelines in YAML, validate
them locally, test them with fixtures, and run them through a modular engine.

[![CI](https://github.com/weavster-dev/weavster/actions/workflows/ci.yml/badge.svg)](https://github.com/weavster-dev/weavster/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/weavster-dev/weavster/branch/main/graph/badge.svg)](https://codecov.io/gh/weavster-dev/weavster)
[![npm](https://img.shields.io/npm/v/@weavster/cli)](https://www.npmjs.com/package/@weavster/cli)
[![License: BUSL-1.1](https://img.shields.io/badge/license-BUSL--1.1-blue)](https://github.com/weavster-dev/weavster/blob/main/LICENSE)
[![Node](https://img.shields.io/node/v/@weavster/cli)](https://nodejs.org)

> Status: config-first authoring, `weavster validate`, fixture-based `weavster test`, the
> v0alpha2 transform DSL, JSON/XML format packs, `weavster run`, and `weavster compile`
> (pipelines → a portable wasm artifact) all work today.

## Quickstart

Install the CLI from npm:

```bash
npm install -g @weavster/cli

weavster init my-integration  # scaffold a project
cd my-integration
weavster validate             # check config + flows
weavster test                 # run fixtures through flows
```

Or work from a clone of this repo (for development):

```bash
pnpm install
pnpm cli:link                 # builds the CLI and links `weavster` globally
```

See the [Getting Started guide](https://docs.weavster.dev/getting-started) for the first
30 minutes, including editing a transform.

## What exists today

- Repository scaffold and folder structure.
- Docusaurus documentation site in [`website/`](website/) with placeholder pages and
  CI to build on PRs and deploy to [docs.weavster.dev](https://docs.weavster.dev) on merge.
- pnpm workspace at the repo root.
- `weavster init [dir]`: scaffolds a minimal starter project (config, a flow, a fixture,
  README) that passes `weavster test` out of the box.
- `weavster validate`: validates a project's `weavster.yaml` against the `v0alpha2`
  schema ([`spec/schemas/project.schema.json`](spec/schemas/project.schema.json)) and each
  `flows/*.yaml` against the flow schema, with path-aware errors.
- `weavster test`: runs each fixture (`fixtures/<flow>/<case>/`) through its
  `flows/<flow>.yaml` and prints a diff for any mismatch against `expected.json`.
- `weavster run [name]`: runs `pipelines/<name>.yaml` — read a source, transform with a flow,
  write a sink (file and stdin/stdout connectors; can convert formats). Omit the name to run all.
- `weavster compile [path]`: compiles the enabled pipelines (the `pipelines:` switchboard in
  `weavster.yaml`) into a portable artifact — `manifest.json` plus one `flows/<flow>.wasm` per
  flow (each flow bundled with the JSON/XML packs and its `_ts` functions, then built to wasm by
  Javy). Output lands in `<project>/target/artifact/`. This is the build step the forthcoming
  Rust engine ([RFC 0003](docs/rfcs/0003-engine-runtime.md)) will run; see
  [`docs/ARTIFACT_SPEC.md`](docs/ARTIFACT_SPEC.md) for the contract.
- A reference user project at [`examples/golden-path/`](examples/golden-path/) exercised
  by `validate` and `test`.
- `@weavster/core`: the canonical document model — a format-agnostic node tree
  (`scalar`/`object`/`array`) with `fromValue`/`toValue` normalization and dotted-path
  access helpers (`get`/`getValue`). See [Concepts](https://docs.weavster.dev/concepts).
- JSON and XML format packs (`@weavster/core` `json` / `xml` namespaces):
  `parse`/`serialize` between text and the canonical model with stable round-tripping. The
  XML pack (fast-xml-parser) maps attributes to `@`-fields, text to `#text`, and repeated
  elements to arrays, plus a pluggable `XmlValidator`. See
  [Format Packs](https://docs.weavster.dev/formats).
- Transform engine (`@weavster/core` `applyFlow`): a `v0alpha2` patch-by-default pipeline over
  the canonical model. Steps are single-key `_op` operators (`_set`/`_default`/`_unset`/
  `_rename`/`_append`/`_select`/`_when`/`_ts`); values are expressions with `$path` references
  and `_op` operators (`_concat`, `_upper`, `_toIso`, `_eq`, `_cond`, …). Driven from
  `flows/*.yaml` via `weavster test`. See [Transform DSL](https://docs.weavster.dev/dsl).
- TypeScript escape hatch (`_ts` step): runs a custom `functions/<module>.ts` (pure JSON in/out,
  loaded via jiti) when the declarative DSL isn't enough. See
  [TypeScript Transforms](https://docs.weavster.dev/typescript).
- Contribution rules ([`CONTRIBUTING.md`](CONTRIBUTING.md)) and PR template.
- Editor/formatter config (`.editorconfig`, Prettier), Biome linter (`biome.json`), and a
  cspell dictionary (`.cspell.json`).
- CodeRabbit reviews configured in `.coderabbit.yaml`: the high-level summary is written into the
  PR description, replacing the `@coderabbitai summary` placeholder the PR template ends with
  (under a `---`). The config also enables the Biome linter and other tools relevant to this repo.
- Engine artifact contract ([`docs/ARTIFACT_SPEC.md`](docs/ARTIFACT_SPEC.md)): the versioned
  `manifest.json` schema ([`spec/schemas/manifest.schema.json`](spec/schemas/manifest.schema.json)),
  the `manifest.json` + `flows/<name>.wasm` artifact layout, and the WASM input/result envelope —
  the contract the Rust engine (RFC 0003) and `weavster compile` are built against. `compile`
  produces this artifact and the engine runs it today.
- Rust engine core ([`engine/`](engine/)): the engine boots from a mounted `weavster.yaml`
  (default `/etc/weavster/weavster.yaml`, `-c/--config` to override) and resolves the compiled
  artifact next to it by convention (`--artifact` to override). It loads + validates the manifest
  (refusing unknown versions loudly), JIT-compiles each flow module once, and runs every pipeline
  concurrently on a tokio runtime — FIFO per pipeline, fresh wasmtime store per document, with a
  memory cap and wall-clock deadline so runaway transforms trap instead of hanging. Structured
  JSON logs carry pipeline/document/stage. Sources and sinks sit behind async `Source`/`Sink`
  traits in a `type`-keyed registry; `file` (glob source, path sink) is the only connector today,
  and later ones are additive — no run-loop change. Ships as a thin multi-stage Docker image
  ([`engine/Dockerfile`](engine/Dockerfile)) — a static-base binary on distroless, no Node.
- Dev log ([`notes/DEV_LOG.md`](notes/DEV_LOG.md)) and changelog
  ([`CHANGELOG.md`](CHANGELOG.md)).

The transform engine is wired into the CLI: `weavster test` runs project flows over their
fixtures and `weavster run` moves real data through them. `init`, `validate`, `test`, `run`,
and `compile` are the working CLI commands.

## Local development

Requires Node 22+ and pnpm.

```bash
pnpm install        # install workspace dependencies
pnpm docs:start     # run the docs site locally
pnpm docs:build     # production build of the docs site
pnpm test           # run all package test suites (core + cli)
pnpm -r coverage    # run every suite with coverage (lcov + text)
pnpm format         # format with Prettier
pnpm format:check   # verify formatting (CI gate)
pnpm lint           # lint with Biome (CI gate)

# A husky pre-commit hook runs Biome + Prettier on staged files (lint-staged),
# so style is fixed automatically before each commit.

# run a command against a project during development
pnpm --filter @weavster/cli dev validate ./path/to/project
pnpm --filter @weavster/cli dev test ./path/to/project
```

### Engine (Rust)

The production runtime ([RFC 0003](docs/rfcs/0003-engine-runtime.md)) lives in
[`engine/`](engine/), a Rust workspace at the repo root. The engine works today: it boots from a
mounted `weavster.yaml` and runs the compiled artifact resolved beside it (manifest loader,
wasmtime host over the Javy ABI, per-pipeline run loop with resource limits, connector registry,
thin Docker image). Only the parity gate lands in a later milestone (see
[`docs/ENGINE_PLAN.md`](docs/ENGINE_PLAN.md)).

```bash
# end to end: compile the golden path, then run the artifact with the engine.
# the engine boots from weavster.yaml and resolves the artifact at
# <config-dir>/target/artifact (matching compile's default output).
pnpm --filter @weavster/cli dev compile ./examples/golden-path
mkdir -p examples/golden-path/target/artifact/in
cp examples/golden-path/in/order.json examples/golden-path/target/artifact/in/
cargo run -- -c examples/golden-path/weavster.yaml

# or as the thin Docker image — mount the project at the default config path.
# the image runs as a non-root user, so run as the host user (--user) to keep
# the bind-mounted sink dir writable.
docker build -f engine/Dockerfile -t weavster-engine .
docker run --rm --user "$(id -u):$(id -g)" \
  -v "$PWD/examples/golden-path:/etc/weavster" weavster-engine
```

**Build boundary:** Rust and the pnpm/TS packages sit side by side but never mix. The TS
toolchain builds the CLI that _produces_ WASM artifacts; the engine only _runs_ them, so no
Node or TS toolchain enters the engine build or its Docker image. Requires a stable Rust
toolchain (`cargo`); Node is not needed to build the engine.

```bash
cargo build --workspace      # build the engine
cargo clippy --workspace --all-targets -- -D warnings
cargo test --workspace       # run engine tests
```

## Layout

| Path          | Purpose                                                    |
| ------------- | ---------------------------------------------------------- |
| `docs/`       | Plan, task list, and (later) the documentation site source |
| `website/`    | Docusaurus docs site (not yet scaffolded)                  |
| `spec/`       | Config JSON Schemas and example configs                    |
| `cli/`        | CLI commands                                               |
| `core/`       | Canonical document model, format packs, and engine         |
| `engine/`     | Rust production runtime (RFC 0003) — currently a stub      |
| `formats/`    | Reserved for format packs if later extracted from `core/`  |
| `functions/`  | Built-in transform functions                               |
| `ts-runtime/` | TypeScript escape hatch for custom transforms              |
| `tests/`      | Fixtures and integration tests                             |
| `examples/`   | Golden-path example project                                |
