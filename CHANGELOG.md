# Changelog

All notable changes to this project are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Add `@biomejs/biome` as a dev dependency so `pnpm lint` runs locally, wire it into CI, and
  clear the findings: drop unused imports, simplify redundant boolean casts, give the escape-hatch
  test functions real types, use a stable React key in the docs homepage, and disable
  `noThenProperty` (the DSL legitimately uses `then`/`else`).

### Added

- RFC 0003 (design only): the Weavster engine — a thin Rust + WASM production runtime. The CLI
  compiles each flow ahead of time (`applyFlow` + format packs + `_ts`, bundled to JS and run
  through Javy/QuickJS) into a per-flow WASM module plus a versioned `manifest.json` artifact;
  the engine loads the artifact, drives connectors, and hosts the WASM. Ships as a thin Docker
  image for single-server now and Kubernetes later. The TS `run` loop stays as the local runtime;
  both reuse the same `@weavster/core` transform code. First slice targets the `file` connector
  behind a pluggable `Source`/`Sink` registry.
- Codecov coverage reporting in CI. Each package exposes a `coverage` script; CI runs
  `pnpm -r coverage` and Codecov auto-discovers every `coverage/lcov.info`, so new JS/TS
  packages (e.g. the UI) are covered without editing CI. `codecov.yml` defines path-based
  components (core, cli, and placeholders for ui/engine) so uncovered packages surface as 0%
  rather than silently missing. A dormant `rust-coverage` CI job auto-activates via
  `cargo-llvm-cov` once a `Cargo.toml` lands. Needs a `CODECOV_TOKEN` repo secret.
- README badges: CI, Codecov, npm, license, Node.
- CI `quality` job enforcing lint (`pnpm lint`) and formatting (`pnpm format:check`) — check-only,
  fails the build on any violation.
- Pre-commit autofix via husky + lint-staged: staged files run Biome (`--write`) and Prettier
  (`--write`) before commit, so style is fixed locally and CI just verifies.
- `.vscode/` workspace settings + recommended extensions (Biome, Prettier) for format-on-save.
- `.cspell.json` spell-check dictionary (seeded with `weavster`), enabled as a CodeRabbit tool
  (CodeRabbit runs cspell server-side during review).
- `weavster run [name]`: execute pipelines that move real data — read a **source**, transform
  with a **flow**, write a **sink**. Pipelines are declared one-per-file in `pipelines/`
  (`source` + `flow` + `sink`); first connectors are `file` and `stdin`/`stdout`. The source
  yields a **stream of documents** and the run loop processes each (a `file` is one document;
  `stdin` is line-delimited and streams). Startup failures abort; per-document failures fail a
  bounded source and are logged on a stream. The source format picks the parser and the sink
  format (defaulting to the source's) picks the serializer, so a pipeline can convert formats.
  `weavster validate` now also checks `pipelines/*.yaml`. Adds a golden-path pipeline, a
  Pipelines docs page, and a CI run smoke. (RFC 0002, slice 1)
- Biome linter config (`biome.json`) plus `lint` / `lint:fix` scripts. Linter only — Prettier
  still owns formatting (Biome's formatter and assist are disabled). CodeRabbit auto-detects the
  config and runs Biome on reviews.
- `@coderabbitai summary` placeholder at the bottom of the PR template (under a `---`), which
  CodeRabbit replaces with a high-level summary in the PR description.
- `.coderabbit.yaml` configuring CodeRabbit reviews: high-level summary in the PR description
  (`high_level_summary_in_walkthrough: false`), `chill` profile, and the linters relevant to this
  repo (Biome, markdownlint, yamllint, actionlint, gitleaks, languagetool).
- RFC 0002 (`docs/rfcs/0002-run-pipelines.md`): design for `weavster run` and config-driven
  pipelines (`pipelines/<name>.yaml` = source + flow + sink), with file and stdin/stdout
  connectors. Draft only — the first "make it move data" phase.

### Changed

- RFC 0002: `weavster run` now runs continuously like an ESB (source yields a stream of
  documents; the loop stays live until end-of-stream) rather than one-shot. `run` with no name
  always runs every pipeline (no `--all` flag). Resolved the `run` default-target open question;
  reworked the data-flow, connector interface (`Source.documents()`), and error handling to match.

## [0.0.3] - 2026-06-06

### Fixed

- The npm package README now renders on npmjs.com. The release workflow publishes from a
  prepared directory (built `dist` + `README.md` + `LICENSE` + a manifest with the workspace
  devDependency and scripts stripped) instead of a pre-packed tarball, so npm populates the
  per-version readme. Also dropped `setup-node`'s `registry-url`, which had forced empty-token
  auth and blocked OIDC trusted publishing.

## [0.0.2] - 2026-06-05

### Added

- `cli/README.md` — the package page shown on npm.

### Changed

- Release workflow now uses **npm trusted publishing (OIDC)** instead of an `NPM_TOKEN` secret:
  it packs with `pnpm pack` and publishes the tarball with `npm publish` under
  `permissions: id-token: write` (no long-lived token).

## [0.0.1] - 2026-06-05

First published release. `@weavster/cli` ships to npm (init / validate / test); the engine,
canonical model, JSON + XML packs, the v0alpha2 transform DSL, and the TypeScript escape hatch
are bundled in.

### Added

- npm release tooling: `@weavster/cli` is published to npm by a tag-triggered (`v*`) GitHub
  Actions release workflow, which also creates a GitHub Release with notes pulled from this
  changelog. The CLI is bundled with tsup — `@weavster/core` and the JSON schemas are inlined —
  so it installs as a single package; runtime deps (ajv, commander, yaml, jiti, fast-xml-parser)
  stay external. CI also typechecks the CLI (tsup does not).

### Fixed

- Pin `types: ["node"]` in `cli/tsconfig.json` so editors resolve Node globals
  (e.g. `process`) through the pnpm symlink.

### Changed

- **Breaking — v0alpha2 DSL cutover.** The CLI now runs flows on the v0alpha2 engine; v0alpha1
  is removed. Flows are single-key `_op` steps with expression values (`$path` refs, `_op`
  operators), patch by default. `weavster.yaml` `apiVersion` is now `weavster/v0alpha2`. Ops:
  `_set`/`_default`/`_unset`/`_rename`/`_append`/`_select`/`_when`/`_ts` plus value operators
  (`_concat`, `_upper`/`_lower`/`_trim`, `_toIso`, `_coalesce`, `_eq`/`_gt`/`_lt`/`_in`,
  `_and`/`_or`/`_not`, `_cond`). The flow schema, golden-path flow, and DSL/Config/TypeScript
  docs are migrated. The single-key step form gives clean validation errors.

### Added

- M9 (slice 2) developer experience: a "first 30 minutes" Getting Started guide, an
  Architecture overview page, a README quickstart, an `init` smoke step in CI, and a release
  checklist (`docs/RELEASE.md`). Completes the MVP plan (M0–M9).
- M9 (slice 1) `weavster init [dir]`: scaffolds a minimal starter project (`weavster.yaml`,
  `flows/main.yaml`, one fixture, `README.md`) that passes `weavster test` immediately. Refuses
  to overwrite an existing project.
- v0alpha2 DSL (slice 2, internal): value operators (`_concat`, `_upper`/`_lower`/`_trim`,
  `_toIso`, `_coalesce`, `_eq`, `_exists`, `_gt`/`_lt`/`_in`, `_and`/`_or`/`_not`, `_cond`) and
  the remaining structural ops (`_rename`, `_append`, `_select` strict projection, `_when` with
  an expression condition). Still internal — not wired into the CLI.
- v0alpha2 DSL (slice 1, internal): expression evaluator (`core/src/dsl/expr.ts`) with `$path`
  references, `$$` escape, deep array/object evaluation, and `_lit`; plus a patch-by-default
  engine (`core/src/dsl/engine.ts`) with the first structural ops `_set` and `_unset`. Not yet
  wired into the CLI — v0alpha1 still runs flows until the cutover slice.
- M8 TypeScript escape hatch: a `ts` transform step runs a custom function from the project's
  `functions/<module>.ts`. The contract is pure JSON in / JSON out (WASM-portable); the core
  engine takes injected functions (`applyFlow(doc, flow, { functions })`) and the CLI loads
  them on demand via jiti. `from`/`to` operate on a subpath (default the whole document).
  Added a golden-path example function, a TypeScript Transforms docs page, and tests.
- RFC 0001 (`docs/rfcs/0001-v0alpha2-dsl.md`): design for the v0alpha2 transform DSL — a
  MongoDB-flavored expression model on a patch-by-default pipeline (`$path` refs, `_op`
  operators), folding in the M7 cleanup. Draft only; targeted post-M8.
- M7 (slice 4) flows wired into the CLI: `weavster test` now parses each fixture's input,
  runs it through `flows/<flow>.yaml`, and compares the output (fixtures are grouped by flow
  under `fixtures/<flow>/<case>/`). `weavster validate` now also validates every `flows/*.yaml`
  against the flow schema. The golden-path example ships a real `flows/order.yaml`. `@weavster/cli`
  now depends on `@weavster/core`.
- M7 (slice 3) conditional op `when` in `@weavster/core`: a `cond` predicate (`path` tested
  with `equals` or `exists`) runs nested `then`/`else` sub-step lists. The pipeline recurses,
  so `when` can nest and nested errors carry the `when` context. Extends `flow.schema.json`
  and the Transform DSL docs.
- M7 (slice 2) transform helper ops in `@weavster/core`: `concat` (join `parts` of paths
  and literals with an optional `sep`), `str` (`upper`/`lower`/`trim`), and `date`
  (`toIso`). Extends `flow.schema.json` and the Transform DSL docs.
- M7 (slice 1) transform engine in `@weavster/core`: `applyFlow` runs an op-keyed step
  list as a mutate-in-place pipeline over the canonical model, with the first operations
  `map`, `rename`, and `default`. Bad mappings raise a step-scoped `TransformError`. Adds
  `set`/`remove` path helpers, a `flow.schema.json` contract with sample flows, and a
  Transform DSL docs page.
- M6 XML format pack in `@weavster/core` (`xml` namespace), built on fast-xml-parser:
  `xml.parse` (well-formedness-checked text → canonical `Document` tagged `xml`,
  `XmlParseError` on malformed input) and `xml.serialize`. Attributes map to `@`-prefixed
  fields, element text to `#text`, repeated elements to arrays; leaf values stay strings.
  Includes a pluggable `XmlValidator` interface (default `wellFormedValidator`, room for
  XSD), round-trip tests, and Format Packs docs with a JSON/XML comparison and limitations.
- M5 JSON format pack in `@weavster/core` (`json` namespace): `json.parse` (text →
  canonical `Document` tagged `json`, `JsonParseError` on invalid input) and
  `json.serialize` (document/node → 2-space JSON with trailing newline). Stable
  round-trip; a richer nested JSON case added to the golden-path example. Format Packs
  docs page added.
- M4 canonical document model in the new `@weavster/core` package: a format-agnostic
  node tree (`scalar`/`object`/`array`) with a `Document` wrapper carrying source format
  and validation messages, `fromValue`/`toValue` normalization, and dotted-path access
  helpers (`parsePath`/`formatPath`/`get`/`getValue`). Concepts page documents it.
- M3 fixture test harness: `weavster test` runs a project's `fixtures/<case>/`
  (`input.json` vs `expected.json`), compares output, and prints a readable diff on
  mismatch. The flow is an identity passthrough until the transform engine lands.
- Reference user project at `examples/golden-path/` exercised by `validate` and `test`.
- `cli:link` root script that builds `@weavster/cli` and links `weavster` globally
  for local use in any folder.
- M2 config validation: `v0alpha1` project schema at `spec/schemas/project.schema.json`.
- `@weavster/cli` package with the `weavster validate` command (commander + Ajv + yaml),
  emitting path-aware error messages.
- Valid and invalid sample configs under `spec/examples/project/` and a vitest suite.
- `ci` GitHub Actions workflow that builds and tests the CLI on PRs and pushes to main.
- M0 reboot foundation: top-level folder structure from `MVP_PLAN.md`.
- `.gitignore`, `.editorconfig`, Prettier config (`.prettierrc.json`, `.prettierignore`).
- `CONTRIBUTING.md` with small-PR, testing, and docs-update rules.
- `.github/PULL_REQUEST_TEMPLATE.md` with docs and test checklist.
- `notes/DEV_LOG.md` work journal with entry template.
- `CHANGELOG.md` and a CLAUDE.md rule to update it with every commit.
- `README.md` describing the current state of the project.
- CLAUDE.md rule to keep `README.md` matching actual (not future) state.
- GitHub milestone labels M0–M9 matching the MVP plan.
- M1 documentation platform: Docusaurus site in `website/` (TypeScript, classic preset).
- pnpm workspace at the repo root with `docs:start` / `docs:build` / `docs:serve` / `format` scripts.
- Documentation IA: explicit sidebar and placeholder pages for Getting Started, Concepts,
  CLI, Config, Testing, Architecture, and Contributing.
- GitHub Actions: build docs on PRs (`docs-build`) and deploy to GitHub Pages on merge (`docs-deploy`).

### Changed

- Serve the docs site from the `docs.weavster.dev` custom domain (`CNAME` + `url`/`baseUrl`).
- Moved planning docs from `mvp/` to `docs/` to match the plan's repo shape.
- Clarified tool-repo vs user-project split in `MVP_PLAN.md`: defined the `weavster init` user-project layout once, separated tool-test fixtures from user-project fixtures, and pinned `examples/golden-path/` as the reference `init` output.
- Patched M9 in `MVP_TASKS.md` to generate the golden-path example via `weavster init` and treat it as the `init` output contract.
