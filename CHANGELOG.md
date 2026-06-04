# Changelog

All notable changes to this project are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- Pin `types: ["node"]` in `cli/tsconfig.json` so editors resolve Node globals
  (e.g. `process`) through the pnpm symlink.

### Added

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
