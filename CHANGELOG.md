# Changelog

All notable changes to this project are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
